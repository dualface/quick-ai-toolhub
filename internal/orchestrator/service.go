package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"quick-ai-toolhub/internal/agentrun"
	toolgit "quick-ai-toolhub/internal/git"
	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/reviewagg"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/timeline"
)

type Service struct {
	logger      *slog.Logger
	store       *store.Service
	github      *toolgithub.Client
	git         *toolgit.Client
	timeline    *timeline.Service
	agentRunner AgentRunner
	reviewAgg   *reviewagg.Service
}

type Dependencies struct {
	Logger      *slog.Logger
	Store       *store.Service
	GitHub      *toolgithub.Client
	Git         *toolgit.Client
	Timeline    *timeline.Service
	AgentRunner AgentRunner
	ReviewAgg   *reviewagg.Service
}

type AgentRunner interface {
	Execute(context.Context, agentrun.Request, agentrun.ExecuteOptions) agentrun.Response
}

type Stage string

const (
	StageDeveloper Stage = "developer"
	StageQA        Stage = "qa"
	StageReview    Stage = "review"
)

const (
	eventDeveloperStarted   = "developer_started"
	eventDeveloperCompleted = "developer_completed"
	eventDeveloperBlocked   = "developer_blocked"
	eventRetryApproved      = "retry_approved"
	eventQAPassed           = "qa_passed"
	eventQAFailed           = "qa_failed"
	eventReviewStarted      = "review_started"
	eventReviewAggregated   = "review_aggregated"
	eventTaskBlocked        = "task_blocked"
	eventTaskAwaitingHuman  = "task_awaiting_human"
	eventTaskEscalated      = "task_escalated"

	defaultReviewerLens        = "correctness"
	defaultMaxStageTransitions = 20
	orchestratorSource         = "orchestrator"
)

type RunTaskOptions struct {
	TaskID              string
	PlanFile            string
	TasksDir            string
	OutputRoot          string
	ConfigFile          string
	Timeout             time.Duration
	Yolo                bool
	IsolatedCodexHome   bool
	ReviewerLens        string
	MaxStageTransitions int
}

type StageResult struct {
	EventID            string                `json:"event_id"`
	Stage              Stage                 `json:"stage"`
	AgentType          agentrun.AgentType    `json:"agent_type"`
	Attempt            int                   `json:"attempt"`
	TaskStatus         string                `json:"task_status"`
	Status             string                `json:"status"`
	Summary            string                `json:"summary"`
	NextAction         string                `json:"next_action"`
	FailureFingerprint string                `json:"failure_fingerprint"`
	ArtifactRefs       agentrun.ArtifactRefs `json:"artifact_refs"`
	Findings           []agentrun.Finding    `json:"findings"`

	// reviewToolDecision holds the structured decision from review-result-tool.
	// This is internal-only and not serialized to JSON.
	reviewToolDecision *reviewToolDecisionMeta `json:"-"`
}

// reviewToolDecisionMeta holds the structured output from review-result-tool.
// The orchestrator consumes this instead of parsing raw reviewer status.
type reviewToolDecisionMeta struct {
	Decision              reviewagg.Decision
	HasCriticalFinding    bool
	HasBlockingFinding    bool
	HasReviewerEscalation bool
	Summary               string
}

type RunTaskResult struct {
	TaskID             string                  `json:"task_id"`
	SprintID           string                  `json:"sprint_id"`
	Stage              Stage                   `json:"stage"`
	Status             string                  `json:"status"`
	Summary            string                  `json:"summary"`
	Attempt            int                     `json:"attempt"`
	FailureFingerprint string                  `json:"failure_fingerprint"`
	ArtifactRefs       agentrun.ArtifactRefs   `json:"artifact_refs"`
	NextAction         string                  `json:"next_action"`
	Task               *store.TaskProjection   `json:"task,omitempty"`
	Sprint             *store.SprintProjection `json:"sprint,omitempty"`
	StageResults       []StageResult           `json:"stage_results"`
}

type taskRuntimeSnapshot struct {
	Projection                store.TaskProjection `bun:"-"`
	TaskID                    string               `bun:"task_id"`
	SprintID                  string               `bun:"sprint_id"`
	TaskLocalID               string               `bun:"task_local_id"`
	SequenceNo                int                  `bun:"sequence_no"`
	GitHubIssueNumber         int                  `bun:"github_issue_number"`
	Status                    string               `bun:"status"`
	AttemptTotal              int                  `bun:"attempt_total"`
	QAFailCount               int                  `bun:"qa_fail_count"`
	ReviewFailCount           int                  `bun:"review_fail_count"`
	CIFailCount               int                  `bun:"ci_fail_count"`
	CurrentFailureFingerprint *string              `bun:"current_failure_fingerprint"`
	ActivePRNumber            *int                 `bun:"active_pr_number"`
	TaskBranch                *string              `bun:"task_branch"`
	WorktreePath              *string              `bun:"worktree_path"`
	NeedsHuman                bool                 `bun:"needs_human"`
	HumanReason               *string              `bun:"human_reason"`
}

func New(deps Dependencies) *Service {
	ra := deps.ReviewAgg
	if ra == nil {
		ra = reviewagg.New()
	}
	return &Service{
		logger:      componentLogger(deps.Logger),
		store:       deps.Store,
		github:      deps.GitHub,
		git:         deps.Git,
		timeline:    deps.Timeline,
		agentRunner: deps.AgentRunner,
		reviewAgg:   ra,
	}
}

func (s *Service) Name() string {
	return "orchestrator"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "orchestrator")
	}
	return logger.With("component", "orchestrator")
}

func currentStageArtifactRefsSnapshot(
	developer agentrun.ArtifactRefs,
	qa agentrun.ArtifactRefs,
	review agentrun.ArtifactRefs,
	lastStage Stage,
	lastStageRefs agentrun.ArtifactRefs,
) stageArtifactRefsSnapshot {
	return stageArtifactRefsSnapshot{
		Developer:             developer,
		QA:                    qa,
		Review:                review,
		LastStage:             lastStage,
		LastStageArtifactRefs: lastStageRefs,
	}
}

func (s *Service) handleStageWriteRejected(
	ctx context.Context,
	taskID string,
	stageResults []StageResult,
	refs stageArtifactRefsSnapshot,
	err error,
) (RunTaskResult, bool, error) {
	var rejected *stageWriteRejectedError
	if !errors.As(err, &rejected) {
		return RunTaskResult{}, false, err
	}

	base, baseErr := s.store.BaseStore()
	if baseErr != nil {
		return RunTaskResult{}, true, baseErr
	}
	sprintProjection, loadErr := base.LoadSprintProjection(ctx, rejected.Current.SprintID)
	if loadErr != nil {
		return RunTaskResult{}, true, loadErr
	}

	return buildNoOpRunTaskResult(rejected.Current, &sprintProjection, stageResults, refs), true, nil
}

func (s *Service) RunTask(ctx context.Context, opts RunTaskOptions) (RunTaskResult, error) {
	if ctx == nil {
		return RunTaskResult{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return RunTaskResult{}, err
	}
	if strings.TrimSpace(opts.TaskID) == "" {
		return RunTaskResult{}, errors.New("task_id is required")
	}
	if s.store == nil {
		return RunTaskResult{}, errors.New("store is required")
	}
	if s.agentRunner == nil {
		return RunTaskResult{}, errors.New("agent runner is required")
	}

	maxTransitions := opts.MaxStageTransitions
	if maxTransitions <= 0 {
		maxTransitions = defaultMaxStageTransitions
	}
	reviewerLens, err := normalizedReviewerLens(opts.ReviewerLens)
	if err != nil {
		return RunTaskResult{}, err
	}
	resumeRefs, err := s.loadLatestStageArtifactRefs(ctx, opts.TaskID)
	if err != nil {
		return RunTaskResult{}, err
	}

	var (
		stageResults      []StageResult
		lastDeveloperRefs = resumeRefs.Developer
		lastQARefs        = resumeRefs.QA
		lastReviewRefs    = resumeRefs.Review
		lastStage         = resumeRefs.LastStage
		lastStageRefs     = resumeRefs.LastStageArtifactRefs
	)

	for transitions := 0; transitions < maxTransitions; transitions++ {
		taskSnapshot, sprintProjection, err := s.loadTaskRuntime(ctx, opts.TaskID)
		if err != nil {
			return RunTaskResult{}, err
		}

		switch strings.TrimSpace(taskSnapshot.Status) {
		case "in_progress", "dev_in_progress":
			taskSnapshot, err = s.enterDeveloperStage(ctx, taskSnapshot, false)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}

			stageResult, err := s.runAgentStage(ctx, StageDeveloper, taskSnapshot, opts, lastQARefs, lastReviewRefs, agentrun.ArtifactRefs{}, "")
			if err != nil {
				return RunTaskResult{}, err
			}
			stageResult, taskSnapshot, err = s.handleDeveloperResult(ctx, taskSnapshot, stageResult)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}
			lastDeveloperRefs = stageResult.ArtifactRefs
			lastStage = stageResult.Stage
			lastStageRefs = stageResult.ArtifactRefs
			stageResults = append(stageResults, stageResult)
			if strings.TrimSpace(taskSnapshot.Status) == "blocked" {
				return buildRunTaskResult(taskSnapshot, sprintProjection, stageResults, stageResult), nil
			}

		case "qa_failed":
			taskSnapshot, err = s.enterDeveloperStage(ctx, taskSnapshot, true)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}

			stageResult, err := s.runAgentStage(ctx, StageDeveloper, taskSnapshot, opts, lastQARefs, lastReviewRefs, agentrun.ArtifactRefs{}, "")
			if err != nil {
				return RunTaskResult{}, err
			}
			stageResult, taskSnapshot, err = s.handleDeveloperResult(ctx, taskSnapshot, stageResult)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}
			lastDeveloperRefs = stageResult.ArtifactRefs
			lastStage = stageResult.Stage
			lastStageRefs = stageResult.ArtifactRefs
			stageResults = append(stageResults, stageResult)
			if strings.TrimSpace(taskSnapshot.Status) == "blocked" {
				return buildRunTaskResult(taskSnapshot, sprintProjection, stageResults, stageResult), nil
			}

		case "qa_in_progress":
			stageResult, err := s.runAgentStage(ctx, StageQA, taskSnapshot, opts, agentrun.ArtifactRefs{}, agentrun.ArtifactRefs{}, lastDeveloperRefs, "")
			if err != nil {
				return RunTaskResult{}, err
			}
			stageResult, taskSnapshot, err = s.handleQAResult(ctx, taskSnapshot, stageResult)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}
			lastQARefs = stageResult.ArtifactRefs
			lastStage = stageResult.Stage
			lastStageRefs = stageResult.ArtifactRefs
			stageResults = append(stageResults, stageResult)

		case "review_failed":
			taskSnapshot, err = s.enterDeveloperStage(ctx, taskSnapshot, true)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}

			stageResult, err := s.runAgentStage(ctx, StageDeveloper, taskSnapshot, opts, lastQARefs, lastReviewRefs, agentrun.ArtifactRefs{}, "")
			if err != nil {
				return RunTaskResult{}, err
			}
			stageResult, taskSnapshot, err = s.handleDeveloperResult(ctx, taskSnapshot, stageResult)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}
			lastDeveloperRefs = stageResult.ArtifactRefs
			lastStage = stageResult.Stage
			lastStageRefs = stageResult.ArtifactRefs
			stageResults = append(stageResults, stageResult)
			if strings.TrimSpace(taskSnapshot.Status) == "blocked" {
				return buildRunTaskResult(taskSnapshot, sprintProjection, stageResults, stageResult), nil
			}

		case "review_in_progress":
			taskSnapshot, err = s.markReviewStarted(ctx, taskSnapshot)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}

			stageResult, reviewFindings, err := s.runReviewStage(ctx, taskSnapshot, opts, reviewerLens, lastQARefs)
			if err != nil {
				return RunTaskResult{}, err
			}
			stageResult, taskSnapshot, err = s.handleReviewResult(ctx, taskSnapshot, stageResult, reviewFindings)
			if err != nil {
				if result, handled, handleErr := s.handleStageWriteRejected(ctx, opts.TaskID, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs), err); handled {
					return result, handleErr
				}
				return RunTaskResult{}, err
			}
			lastReviewRefs = stageResult.ArtifactRefs
			lastStage = stageResult.Stage
			lastStageRefs = stageResult.ArtifactRefs
			stageResults = append(stageResults, stageResult)
			if strings.TrimSpace(taskSnapshot.Status) == "pr_open" || strings.TrimSpace(taskSnapshot.Status) == "awaiting_human" {
				return buildRunTaskResult(taskSnapshot, sprintProjection, stageResults, stageResult), nil
			}

		case "pr_open", "blocked", "escalated", "awaiting_human", "done", "canceled":
			return buildNoOpRunTaskResult(taskSnapshot, sprintProjection, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs)), nil

		default:
			return RunTaskResult{}, fmt.Errorf("task %s status %s is not supported by the stage machine", taskSnapshot.TaskID, strings.TrimSpace(taskSnapshot.Status))
		}
	}

	return s.handleTransitionBudgetExceeded(ctx, opts.TaskID, maxTransitions, stageResults, currentStageArtifactRefsSnapshot(lastDeveloperRefs, lastQARefs, lastReviewRefs, lastStage, lastStageRefs))
}
