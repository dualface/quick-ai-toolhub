package githubsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/store"
)

const (
	defaultIssueLimit = 500
	defaultPRLimit    = 500
	defaultRunLimit   = 500
)

type GitHubReader interface {
	ListSprintIssues(ctx context.Context, req toolgithub.ListSprintIssuesRequest) ([]toolgithub.Issue, error)
	ListIssues(ctx context.Context, req toolgithub.ListIssuesRequest) ([]toolgithub.Issue, error)
	GetIssue(ctx context.Context, req toolgithub.GetIssueRequest) (toolgithub.Issue, error)
	ListSubIssues(ctx context.Context, req toolgithub.ListSubIssuesRequest) ([]toolgithub.IssueLink, error)
	ListIssueDependencies(ctx context.Context, req toolgithub.ListIssueDependenciesRequest) ([]toolgithub.IssueLink, error)
	ListPullRequests(ctx context.Context, req toolgithub.ListPullRequestsRequest) ([]toolgithub.PullRequest, error)
	GetPullRequest(ctx context.Context, req toolgithub.GetPullRequestRequest) (toolgithub.PullRequest, error)
	ListWorkflowRuns(ctx context.Context, req toolgithub.ListWorkflowRunsRequest) ([]toolgithub.WorkflowRun, error)
	GetWorkflowRun(ctx context.Context, req toolgithub.GetWorkflowRunRequest) (toolgithub.WorkflowRun, error)
}

type ProjectionStore interface {
	ApplyGitHubProjection(ctx context.Context, snapshot store.GitHubProjectionSnapshot) error
	RunInTx(ctx context.Context, fn func(context.Context, store.BaseStore) error) error
	FindTrackedSprintIssueNumbersForIssue(ctx context.Context, githubIssueNumber int) ([]int, error)
	FindTrackedSprintIssueNumberBySprintID(ctx context.Context, sprintID string) (int, bool, error)
	FindTrackedSprintIssueNumberByPullRequest(ctx context.Context, githubPRNumber int) (int, bool, error)
	FindTrackedSprintIssueNumberByCIRun(ctx context.Context, githubRunID int64) (int, bool, error)
	FindTrackedSprintIssueNumberByHead(ctx context.Context, headSHA, headBranch string) (int, bool, error)
}

type Service struct {
	logger *slog.Logger
	github GitHubReader
	store  ProjectionStore
}

type Dependencies struct {
	Logger *slog.Logger
	GitHub GitHubReader
	Store  ProjectionStore
}

func New(deps Dependencies) *Service {
	return &Service{
		logger: componentLogger(deps.Logger),
		github: deps.GitHub,
		store:  deps.Store,
	}
}

func (s *Service) Name() string {
	return "githubsync"
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "githubsync")
	}
	return logger.With("component", "githubsync")
}

func (s *Service) Execute(ctx context.Context, req Request, opts ExecuteOptions) Response {
	startedAt := currentUTCTimestamp()
	if s == nil {
		return Response{
			OK: false,
			Error: &ToolError{
				Code:      ErrorCodeInternal,
				Message:   "githubsync service is nil",
				Retryable: false,
			},
		}
	}

	changedEntities, err := s.execute(ctx, req, opts)
	finishedAt := currentUTCTimestamp()
	if err != nil {
		return Response{
			OK:    false,
			Error: asToolError(err),
		}
	}

	return Response{
		OK: true,
		Data: &ResponseData{
			SyncSummary: SyncSummary{
				Op:           string(req.Op),
				StartedAt:    startedAt,
				FinishedAt:   finishedAt,
				ChangedCount: len(changedEntities),
			},
			ChangedEntities: changedEntities,
		},
	}
}

func (s *Service) execute(ctx context.Context, req Request, opts ExecuteOptions) ([]ChangedEntity, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := validateExecuteOptions(opts); err != nil {
		return nil, err
	}
	if s.github == nil {
		return nil, errors.New("github reader is required")
	}
	if s.store == nil {
		return nil, errors.New("projection store is required")
	}

	switch req.Op {
	case OpFullReconcile:
		var payload FullReconcilePayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, err
		}
		return s.fullReconcile(ctx, payload, opts)
	case OpIngestWebhook:
		var payload IngestWebhookPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, err
		}
		return s.ingestWebhook(ctx, payload, opts)
	case OpReconcileIssue:
		var payload ReconcileIssuePayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, err
		}
		return s.reconcileIssue(ctx, payload, opts)
	case OpReconcilePullReq:
		var payload ReconcilePullRequestPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, err
		}
		return s.reconcilePullRequest(ctx, payload, opts)
	case OpReconcileCIRun:
		var payload ReconcileCIRunPayload
		if err := decodePayload(req.Payload, &payload); err != nil {
			return nil, err
		}
		return s.reconcileCIRun(ctx, payload, opts)
	case "":
		return nil, newValidationError("op is required")
	default:
		return nil, newValidationError("unsupported op %q", req.Op)
	}
}

func (s *Service) fullReconcile(ctx context.Context, payload FullReconcilePayload, opts ExecuteOptions) ([]ChangedEntity, error) {
	reason := strings.TrimSpace(payload.Reason)
	switch reason {
	case "startup", "periodic", "manual":
	default:
		return nil, newValidationError("full_reconcile.reason must be one of startup, periodic, manual")
	}

	scope := toolgithub.Scope{
		WorkDir: opts.WorkDir,
		Repo:    opts.Repo,
	}

	sprintIssues, err := s.github.ListSprintIssues(ctx, toolgithub.ListSprintIssuesRequest{
		Scope: scope,
		Limit: defaultIssueLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sprint issues: %w", err)
	}

	openTaskIssues, err := s.github.ListIssues(ctx, toolgithub.ListIssuesRequest{
		Scope:  scope,
		State:  "open",
		Labels: []string{"kind/task"},
		Limit:  defaultIssueLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list open task issues: %w", err)
	}

	parsedSprints, err := s.loadSprints(ctx, scope, sprintIssues)
	if err != nil {
		return nil, err
	}

	parsedTasks, dependencies, err := s.loadTasksAndDependencies(ctx, scope, parsedSprints, openTaskIssues)
	if err != nil {
		return nil, err
	}

	snapshot, changedEntities, err := s.buildSnapshot(ctx, scope, opts.DefaultBranch, reason, parsedSprints, parsedTasks, dependencies)
	if err != nil {
		return nil, err
	}

	if err := s.store.ApplyGitHubProjection(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("apply github projection: %w", err)
	}

	return changedEntities, nil
}

func (s *Service) ingestWebhook(ctx context.Context, payload IngestWebhookPayload, opts ExecuteOptions) ([]ChangedEntity, error) {
	if err := validateIngestWebhookPayload(payload); err != nil {
		return nil, err
	}

	action, err := decodeWebhookAction(payload)
	if err != nil {
		return nil, err
	}

	var changedEntities []ChangedEntity
	err = s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		appendResult, err := tx.AppendEvent(ctx, webhookEventPayload(payload, action))
		if err != nil {
			return err
		}
		if appendResult.Deduplicated || action.Reconcile == nil {
			return nil
		}

		snapshot, entities, err := s.targetedSnapshot(ctx, action.Reconcile.Op, action.Reconcile.Payload, opts, action.Reconcile.Reason)
		if err != nil {
			return err
		}
		if len(entities) == 0 || len(snapshot.Sprints) == 0 {
			return nil
		}
		if err := tx.ApplyGitHubProjection(ctx, snapshot); err != nil {
			return fmt.Errorf("apply github projection: %w", err)
		}
		changedEntities = entities
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changedEntities, nil
}

func (s *Service) reconcileIssue(ctx context.Context, payload ReconcileIssuePayload, opts ExecuteOptions) ([]ChangedEntity, error) {
	if payload.GitHubIssueNumber <= 0 {
		return nil, newValidationError("reconcile_issue.github_issue_number must be greater than zero")
	}
	return s.applyTargetedReconcile(ctx, OpReconcileIssue, payload, opts, "targeted_issue")
}

func (s *Service) reconcilePullRequest(ctx context.Context, payload ReconcilePullRequestPayload, opts ExecuteOptions) ([]ChangedEntity, error) {
	if payload.GitHubPRNumber <= 0 {
		return nil, newValidationError("reconcile_pull_request.github_pr_number must be greater than zero")
	}
	return s.applyTargetedReconcile(ctx, OpReconcilePullReq, payload, opts, "targeted_pull_request")
}

func (s *Service) reconcileCIRun(ctx context.Context, payload ReconcileCIRunPayload, opts ExecuteOptions) ([]ChangedEntity, error) {
	if payload.GitHubRunID <= 0 {
		return nil, newValidationError("reconcile_ci_run.github_run_id must be greater than zero")
	}
	return s.applyTargetedReconcile(ctx, OpReconcileCIRun, payload, opts, "targeted_ci_run")
}

func (s *Service) applyTargetedReconcile(ctx context.Context, op Operation, payload any, opts ExecuteOptions, reason string) ([]ChangedEntity, error) {
	snapshot, changedEntities, err := s.targetedSnapshot(ctx, op, payload, opts, reason)
	if err != nil {
		return nil, err
	}
	if len(changedEntities) == 0 || len(snapshot.Sprints) == 0 {
		return []ChangedEntity{}, nil
	}
	if err := s.store.ApplyGitHubProjection(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("apply github projection: %w", err)
	}
	return changedEntities, nil
}

func (s *Service) targetedSnapshot(
	ctx context.Context,
	op Operation,
	payload any,
	opts ExecuteOptions,
	reason string,
) (store.GitHubProjectionSnapshot, []ChangedEntity, error) {
	scope := toolgithub.Scope{
		WorkDir: opts.WorkDir,
		Repo:    opts.Repo,
	}

	var (
		sprintIssueNums []int
		err             error
	)
	switch op {
	case OpReconcileIssue:
		sprintIssueNums, err = s.resolveSprintIssueNumbersForIssue(ctx, scope, payload.(ReconcileIssuePayload).GitHubIssueNumber)
	case OpReconcilePullReq:
		sprintIssueNums, err = s.resolveSprintIssueNumbersForPullRequest(ctx, scope, payload.(ReconcilePullRequestPayload).GitHubPRNumber)
	case OpReconcileCIRun:
		sprintIssueNums, err = s.resolveSprintIssueNumbersForWorkflowRun(ctx, scope, payload.(ReconcileCIRunPayload).GitHubRunID)
	default:
		return store.GitHubProjectionSnapshot{}, nil, newValidationError("unsupported op %q", op)
	}
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, err
	}
	if len(sprintIssueNums) == 0 {
		return store.GitHubProjectionSnapshot{}, []ChangedEntity{}, nil
	}

	snapshot, changedEntities, err := s.buildScopedSnapshot(ctx, scope, opts.DefaultBranch, reason, sprintIssueNums)
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, err
	}
	return snapshot, changedEntities, nil
}

func (s *Service) buildScopedSnapshot(
	ctx context.Context,
	scope toolgithub.Scope,
	defaultBranch string,
	reason string,
	sprintIssueNumbers []int,
) (store.GitHubProjectionSnapshot, []ChangedEntity, error) {
	parsedSprints, err := s.loadScopedSprints(ctx, scope, sprintIssueNumbers)
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, err
	}

	parsedTasks, dependencies, err := s.loadTasksAndDependencies(ctx, scope, parsedSprints, nil)
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, err
	}

	return s.buildSnapshot(ctx, scope, defaultBranch, reason, parsedSprints, parsedTasks, dependencies)
}

func (s *Service) loadScopedSprints(ctx context.Context, scope toolgithub.Scope, sprintIssueNumbers []int) ([]parsedSprint, error) {
	parsed := make([]parsedSprint, 0, len(sprintIssueNumbers))
	sprintIDs := make(map[string]int, len(sprintIssueNumbers))
	sprintSequences := make(map[int]int, len(sprintIssueNumbers))
	seenIssues := make(map[int]struct{}, len(sprintIssueNumbers))

	for _, sprintIssueNumber := range sprintIssueNumbers {
		if sprintIssueNumber <= 0 {
			continue
		}
		if _, ok := seenIssues[sprintIssueNumber]; ok {
			continue
		}
		seenIssues[sprintIssueNumber] = struct{}{}

		issue, err := s.github.GetIssue(ctx, toolgithub.GetIssueRequest{
			Scope:             scope,
			GitHubIssueNumber: sprintIssueNumber,
		})
		if err != nil {
			return nil, fmt.Errorf("get sprint issue #%d: %w", sprintIssueNumber, err)
		}

		item, err := parseSprintIssue(issue)
		if err != nil {
			return nil, err
		}
		if existing, ok := sprintIDs[item.Snapshot.SprintID]; ok {
			return nil, newValidationError(
				"duplicate sprint id %s on issues #%d and #%d",
				item.Snapshot.SprintID,
				existing,
				item.Issue.GitHubIssueNumber,
			)
		}
		if existing, ok := sprintSequences[item.Snapshot.SequenceNo]; ok {
			return nil, newValidationError(
				"duplicate sprint sequence %d on issues #%d and #%d",
				item.Snapshot.SequenceNo,
				existing,
				item.Issue.GitHubIssueNumber,
			)
		}

		subIssues, err := s.github.ListSubIssues(ctx, toolgithub.ListSubIssuesRequest{
			Scope:             scope,
			ParentIssueNumber: sprintIssueNumber,
		})
		if err != nil {
			return nil, fmt.Errorf("list sub-issues for sprint #%d: %w", sprintIssueNumber, err)
		}
		seenSubIssues := make(map[int]struct{}, len(subIssues))
		for _, subIssue := range subIssues {
			if _, ok := seenSubIssues[subIssue.GitHubIssueNumber]; ok {
				continue
			}
			seenSubIssues[subIssue.GitHubIssueNumber] = struct{}{}
			item.TaskIssueNumbers = append(item.TaskIssueNumbers, subIssue.GitHubIssueNumber)
		}

		sprintIDs[item.Snapshot.SprintID] = item.Issue.GitHubIssueNumber
		sprintSequences[item.Snapshot.SequenceNo] = item.Issue.GitHubIssueNumber
		parsed = append(parsed, item)
	}

	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].Snapshot.SequenceNo < parsed[j].Snapshot.SequenceNo
	})
	return parsed, nil
}

func (s *Service) resolveSprintIssueNumbersForIssue(ctx context.Context, scope toolgithub.Scope, githubIssueNumber int) ([]int, error) {
	values, err := s.store.FindTrackedSprintIssueNumbersForIssue(ctx, githubIssueNumber)
	if err != nil {
		return nil, err
	}

	issue, err := s.github.GetIssue(ctx, toolgithub.GetIssueRequest{
		Scope:             scope,
		GitHubIssueNumber: githubIssueNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("get issue %d: %w", githubIssueNumber, err)
	}

	if issue.HasLabel("kind/sprint") {
		values = append(values, githubIssueNumber)
		return dedupeSprintIssueNumbers(values), nil
	}

	parents, err := s.findOpenParentSprintIssueNumbersForTask(ctx, scope, githubIssueNumber)
	if err != nil {
		return nil, err
	}
	values = append(values, parents...)
	return dedupeSprintIssueNumbers(values), nil
}

func (s *Service) resolveSprintIssueNumbersForPullRequest(ctx context.Context, scope toolgithub.Scope, githubPRNumber int) ([]int, error) {
	values := make([]int, 0, 2)
	if tracked, ok, err := s.store.FindTrackedSprintIssueNumberByPullRequest(ctx, githubPRNumber); err != nil {
		return nil, err
	} else if ok {
		values = append(values, tracked)
	}

	pr, err := s.github.GetPullRequest(ctx, toolgithub.GetPullRequestRequest{
		Scope:          scope,
		GitHubPRNumber: githubPRNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("get pull request %d: %w", githubPRNumber, err)
	}

	if sprintID, _, ok := parseTaskBranch(pr.HeadBranch); ok {
		value, ok, err := s.resolveSprintIssueNumberBySprintID(ctx, scope, sprintID)
		if err != nil {
			return nil, err
		}
		if ok {
			values = append(values, value)
		}
	} else if sprintID, ok := parseSprintBranch(pr.HeadBranch); ok {
		value, ok, err := s.resolveSprintIssueNumberBySprintID(ctx, scope, sprintID)
		if err != nil {
			return nil, err
		}
		if ok {
			values = append(values, value)
		}
	}

	return dedupeSprintIssueNumbers(values), nil
}

func (s *Service) resolveSprintIssueNumbersForWorkflowRun(ctx context.Context, scope toolgithub.Scope, githubRunID int64) ([]int, error) {
	values := make([]int, 0, 2)
	if tracked, ok, err := s.store.FindTrackedSprintIssueNumberByCIRun(ctx, githubRunID); err != nil {
		return nil, err
	} else if ok {
		values = append(values, tracked)
	}

	run, err := s.github.GetWorkflowRun(ctx, toolgithub.GetWorkflowRunRequest{
		Scope:       scope,
		GitHubRunID: githubRunID,
	})
	if err != nil {
		return nil, fmt.Errorf("get workflow run %d: %w", githubRunID, err)
	}

	if sprintID, _, ok := parseTaskBranch(run.HeadBranch); ok {
		value, ok, err := s.resolveSprintIssueNumberBySprintID(ctx, scope, sprintID)
		if err != nil {
			return nil, err
		}
		if ok {
			values = append(values, value)
		}
	} else if sprintID, ok := parseSprintBranch(run.HeadBranch); ok {
		value, ok, err := s.resolveSprintIssueNumberBySprintID(ctx, scope, sprintID)
		if err != nil {
			return nil, err
		}
		if ok {
			values = append(values, value)
		}
	} else if tracked, ok, err := s.store.FindTrackedSprintIssueNumberByHead(ctx, run.HeadSHA, run.HeadBranch); err != nil {
		return nil, err
	} else if ok {
		values = append(values, tracked)
	}

	return dedupeSprintIssueNumbers(values), nil
}

func (s *Service) resolveSprintIssueNumberBySprintID(ctx context.Context, scope toolgithub.Scope, sprintID string) (int, bool, error) {
	if tracked, ok, err := s.store.FindTrackedSprintIssueNumberBySprintID(ctx, sprintID); err != nil {
		return 0, false, err
	} else if ok {
		return tracked, true, nil
	}

	sprintIssues, err := s.github.ListSprintIssues(ctx, toolgithub.ListSprintIssuesRequest{
		Scope: scope,
		Limit: defaultIssueLimit,
	})
	if err != nil {
		return 0, false, fmt.Errorf("list sprint issues: %w", err)
	}
	for _, issue := range sprintIssues {
		parsed, err := parseSprintIssue(issue)
		if err != nil {
			return 0, false, err
		}
		if parsed.Snapshot.SprintID == sprintID {
			return issue.GitHubIssueNumber, true, nil
		}
	}
	return 0, false, nil
}

func (s *Service) findOpenParentSprintIssueNumbersForTask(ctx context.Context, scope toolgithub.Scope, taskIssueNumber int) ([]int, error) {
	sprintIssues, err := s.github.ListSprintIssues(ctx, toolgithub.ListSprintIssuesRequest{
		Scope: scope,
		Limit: defaultIssueLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sprint issues: %w", err)
	}

	values := make([]int, 0, 1)
	for _, sprintIssue := range sprintIssues {
		subIssues, err := s.github.ListSubIssues(ctx, toolgithub.ListSubIssuesRequest{
			Scope:             scope,
			ParentIssueNumber: sprintIssue.GitHubIssueNumber,
		})
		if err != nil {
			return nil, fmt.Errorf("list sub-issues for sprint #%d: %w", sprintIssue.GitHubIssueNumber, err)
		}
		for _, subIssue := range subIssues {
			if subIssue.GitHubIssueNumber == taskIssueNumber {
				values = append(values, sprintIssue.GitHubIssueNumber)
				break
			}
		}
	}
	return dedupeSprintIssueNumbers(values), nil
}

func dedupeSprintIssueNumbers(values []int) []int {
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func (s *Service) loadSprints(ctx context.Context, scope toolgithub.Scope, sprintIssues []toolgithub.Issue) ([]parsedSprint, error) {
	parsed := make([]parsedSprint, 0, len(sprintIssues))
	sprintIDs := make(map[string]int, len(sprintIssues))
	sprintSequences := make(map[int]int, len(sprintIssues))

	for _, issue := range sprintIssues {
		item, err := parseSprintIssue(issue)
		if err != nil {
			return nil, err
		}
		if existing, ok := sprintIDs[item.Snapshot.SprintID]; ok {
			return nil, newValidationError(
				"duplicate sprint id %s on issues #%d and #%d",
				item.Snapshot.SprintID,
				existing,
				item.Issue.GitHubIssueNumber,
			)
		}
		if existing, ok := sprintSequences[item.Snapshot.SequenceNo]; ok {
			return nil, newValidationError(
				"duplicate sprint sequence %d on issues #%d and #%d",
				item.Snapshot.SequenceNo,
				existing,
				item.Issue.GitHubIssueNumber,
			)
		}

		subIssues, err := s.github.ListSubIssues(ctx, toolgithub.ListSubIssuesRequest{
			Scope:             scope,
			ParentIssueNumber: issue.GitHubIssueNumber,
		})
		if err != nil {
			return nil, fmt.Errorf("list sub-issues for sprint #%d: %w", issue.GitHubIssueNumber, err)
		}

		seenSubIssues := make(map[int]struct{}, len(subIssues))
		for _, subIssue := range subIssues {
			if _, ok := seenSubIssues[subIssue.GitHubIssueNumber]; ok {
				continue
			}
			seenSubIssues[subIssue.GitHubIssueNumber] = struct{}{}
			item.TaskIssueNumbers = append(item.TaskIssueNumbers, subIssue.GitHubIssueNumber)
		}

		sprintIDs[item.Snapshot.SprintID] = item.Issue.GitHubIssueNumber
		sprintSequences[item.Snapshot.SequenceNo] = item.Issue.GitHubIssueNumber
		parsed = append(parsed, item)
	}

	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].Snapshot.SequenceNo < parsed[j].Snapshot.SequenceNo
	})
	return parsed, nil
}

func (s *Service) loadTasksAndDependencies(
	ctx context.Context,
	scope toolgithub.Scope,
	sprints []parsedSprint,
	openTaskIssues []toolgithub.Issue,
) ([]parsedTask, []store.TaskDependencySnapshot, error) {
	parentByTaskIssue := make(map[int]parsedSprint)
	taskIssueNumbers := make(map[int]struct{})
	for _, sprint := range sprints {
		for _, taskIssueNumber := range sprint.TaskIssueNumbers {
			if parent, ok := parentByTaskIssue[taskIssueNumber]; ok && parent.Snapshot.SprintID != sprint.Snapshot.SprintID {
				return nil, nil, newValidationError(
					"task issue #%d is linked under multiple sprint issues: %s and %s",
					taskIssueNumber,
					parent.Snapshot.SprintID,
					sprint.Snapshot.SprintID,
				)
			}
			parentByTaskIssue[taskIssueNumber] = sprint
			taskIssueNumbers[taskIssueNumber] = struct{}{}
		}
	}

	openTaskByIssue := make(map[int]toolgithub.Issue, len(openTaskIssues))
	for _, issue := range openTaskIssues {
		openTaskByIssue[issue.GitHubIssueNumber] = issue
	}

	parsedTasks := make([]parsedTask, 0, len(taskIssueNumbers))
	tasksByIssueNumber := make(map[int]parsedTask, len(taskIssueNumbers))
	tasksByID := make(map[string]int, len(taskIssueNumbers))
	taskSequencesBySprint := make(map[string]map[int]int)

	for taskIssueNumber, parentSprint := range parentByTaskIssue {
		issue, err := s.github.GetIssue(ctx, toolgithub.GetIssueRequest{
			Scope:             scope,
			GitHubIssueNumber: taskIssueNumber,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("get task issue #%d: %w", taskIssueNumber, err)
		}

		task, err := parseTaskIssue(issue, parentSprint.Snapshot.SprintID, parentSprint.Issue.GitHubIssueNumber)
		if err != nil {
			return nil, nil, err
		}

		if existing, ok := tasksByID[task.Snapshot.TaskID]; ok {
			return nil, nil, newValidationError(
				"duplicate task id %s on issues #%d and #%d",
				task.Snapshot.TaskID,
				existing,
				taskIssueNumber,
			)
		}

		sequenceBySprint := taskSequencesBySprint[task.Snapshot.SprintID]
		if sequenceBySprint == nil {
			sequenceBySprint = make(map[int]int)
			taskSequencesBySprint[task.Snapshot.SprintID] = sequenceBySprint
		}
		if existing, ok := sequenceBySprint[task.Snapshot.SequenceNo]; ok {
			return nil, nil, newValidationError(
				"duplicate task sequence %d in %s on issues #%d and #%d",
				task.Snapshot.SequenceNo,
				task.Snapshot.SprintID,
				existing,
				taskIssueNumber,
			)
		}

		dependencyLinks, err := s.github.ListIssueDependencies(ctx, toolgithub.ListIssueDependenciesRequest{
			Scope:             scope,
			GitHubIssueNumber: taskIssueNumber,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("list dependencies for task issue #%d: %w", taskIssueNumber, err)
		}
		seenDependencies := make(map[int]struct{}, len(dependencyLinks))
		for _, link := range dependencyLinks {
			if _, ok := seenDependencies[link.GitHubIssueNumber]; ok {
				continue
			}
			seenDependencies[link.GitHubIssueNumber] = struct{}{}
			task.DependencyIssueNumbers = append(task.DependencyIssueNumbers, link.GitHubIssueNumber)
		}

		tasksByID[task.Snapshot.TaskID] = taskIssueNumber
		sequenceBySprint[task.Snapshot.SequenceNo] = taskIssueNumber
		tasksByIssueNumber[taskIssueNumber] = task
		parsedTasks = append(parsedTasks, task)
	}

	for issueNumber, issue := range openTaskByIssue {
		if _, ok := parentByTaskIssue[issueNumber]; !ok {
			return nil, nil, newValidationError(
				"open task issue #%d (%s) is orphaned and not attached to any sprint issue",
				issueNumber,
				issue.Title,
			)
		}
	}

	dependencies := make([]store.TaskDependencySnapshot, 0)
	seenDependencyEdges := make(map[string]struct{})
	for _, task := range parsedTasks {
		for _, dependencyIssueNumber := range task.DependencyIssueNumbers {
			dependencyTask, ok := tasksByIssueNumber[dependencyIssueNumber]
			if !ok {
				return nil, nil, newValidationError(
					"task %s depends on issue #%d which is not projected under any open sprint",
					task.Snapshot.TaskID,
					dependencyIssueNumber,
				)
			}
			if dependencyTask.Snapshot.SprintID != task.Snapshot.SprintID {
				return nil, nil, newValidationError(
					"task %s has cross-sprint dependency on %s",
					task.Snapshot.TaskID,
					dependencyTask.Snapshot.TaskID,
				)
			}
			key := task.Snapshot.TaskID + "->" + dependencyTask.Snapshot.TaskID
			if _, ok := seenDependencyEdges[key]; ok {
				continue
			}
			seenDependencyEdges[key] = struct{}{}
			dependencies = append(dependencies, store.TaskDependencySnapshot{
				TaskID:          task.Snapshot.TaskID,
				DependsOnTaskID: dependencyTask.Snapshot.TaskID,
				Source:          "github_issue_dependency",
			})
		}
	}

	sort.Slice(parsedTasks, func(i, j int) bool {
		if parsedTasks[i].Snapshot.SprintID != parsedTasks[j].Snapshot.SprintID {
			return parsedTasks[i].Snapshot.SprintID < parsedTasks[j].Snapshot.SprintID
		}
		return parsedTasks[i].Snapshot.SequenceNo < parsedTasks[j].Snapshot.SequenceNo
	})
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].TaskID != dependencies[j].TaskID {
			return dependencies[i].TaskID < dependencies[j].TaskID
		}
		return dependencies[i].DependsOnTaskID < dependencies[j].DependsOnTaskID
	})

	return parsedTasks, dependencies, nil
}

func (s *Service) buildSnapshot(
	ctx context.Context,
	scope toolgithub.Scope,
	defaultBranch string,
	reason string,
	sprints []parsedSprint,
	tasks []parsedTask,
	dependencies []store.TaskDependencySnapshot,
) (store.GitHubProjectionSnapshot, []ChangedEntity, error) {
	syncedAt := currentUTCTimestamp()

	sprintsByID := make(map[string]parsedSprint, len(sprints))
	tasksByID := make(map[string]parsedTask, len(tasks))
	for _, sprint := range sprints {
		sprintsByID[sprint.Snapshot.SprintID] = sprint
	}
	for _, task := range tasks {
		tasksByID[task.Snapshot.TaskID] = task
	}

	pullRequests, err := s.github.ListPullRequests(ctx, toolgithub.ListPullRequestsRequest{
		Scope: scope,
		State: "all",
		Limit: defaultPRLimit,
	})
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, fmt.Errorf("list pull requests: %w", err)
	}

	projectedPRs, projectedPRMap, err := projectPullRequests(pullRequests, sprintsByID, tasksByID, defaultBranch, syncedAt)
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, err
	}

	runs, err := s.github.ListWorkflowRuns(ctx, toolgithub.ListWorkflowRunsRequest{
		Scope: scope,
		Limit: defaultRunLimit,
	})
	if err != nil {
		return store.GitHubProjectionSnapshot{}, nil, fmt.Errorf("list workflow runs: %w", err)
	}

	projectedRuns := projectWorkflowRuns(runs, projectedPRMap, sprintsByID, tasksByID, syncedAt)

	sprintSnapshots := make([]store.SprintDefinitionSnapshot, 0, len(sprints))
	taskSnapshots := make([]store.TaskDefinitionSnapshot, 0, len(tasks))
	for _, sprint := range sprints {
		sprintSnapshots = append(sprintSnapshots, sprint.Snapshot)
	}
	for _, task := range tasks {
		taskSnapshots = append(taskSnapshots, task.Snapshot)
	}

	changedEntities := make([]ChangedEntity, 0, len(sprintSnapshots)+len(taskSnapshots)+len(projectedPRs)+len(projectedRuns))
	for _, sprint := range sprintSnapshots {
		changedEntities = append(changedEntities, ChangedEntity{
			EntityType: "sprint",
			EntityID:   sprint.SprintID,
		})
	}
	for _, task := range taskSnapshots {
		changedEntities = append(changedEntities, ChangedEntity{
			EntityType: "task",
			EntityID:   task.TaskID,
		})
	}
	for _, pr := range projectedPRs {
		changedEntities = append(changedEntities, ChangedEntity{
			EntityType: "pull_request",
			EntityID:   fmt.Sprintf("%d", pr.GitHubPRNumber),
		})
	}
	for _, run := range projectedRuns {
		changedEntities = append(changedEntities, ChangedEntity{
			EntityType: "ci_run",
			EntityID:   fmt.Sprintf("%d", run.GitHubRunID),
		})
	}
	sortChangedEntities(changedEntities)

	return store.GitHubProjectionSnapshot{
		Reason:           reason,
		SyncedAt:         syncedAt,
		RepoConfig:       repoConfigSnapshot(scope.Repo, defaultBranch),
		Sprints:          sprintSnapshots,
		Tasks:            taskSnapshots,
		TaskDependencies: dependencies,
		PullRequests:     projectedPRs,
		CIRuns:           projectedRuns,
	}, changedEntities, nil
}

func projectPullRequests(
	pullRequests []toolgithub.PullRequest,
	sprints map[string]parsedSprint,
	tasks map[string]parsedTask,
	defaultBranch string,
	syncedAt string,
) ([]store.PullRequestSnapshot, map[int]store.PullRequestSnapshot, error) {
	projected := make([]store.PullRequestSnapshot, 0)
	byNumber := make(map[int]store.PullRequestSnapshot)

	for _, pr := range pullRequests {
		var snapshot store.PullRequestSnapshot
		if sprintID, taskID, ok := parseTaskBranch(pr.HeadBranch); ok {
			task, taskOK := tasks[taskID]
			if !taskOK {
				if _, sprintOK := sprints[sprintID]; sprintOK {
					return nil, nil, newValidationError("pull request #%d references unknown task branch %s", pr.GitHubPRNumber, pr.HeadBranch)
				}
				continue
			}
			snapshot = store.PullRequestSnapshot{
				GitHubPRNumber:   pr.GitHubPRNumber,
				GitHubPRNodeID:   pr.GitHubPRNodeID,
				PRKind:           "task",
				SprintID:         task.Snapshot.SprintID,
				TaskID:           optionalText(task.Snapshot.TaskID),
				HeadBranch:       pr.HeadBranch,
				BaseBranch:       pr.BaseBranch,
				Status:           pr.State,
				AutoMergeEnabled: pr.AutoMergeEnabled,
				HeadSHA:          pr.HeadSHA,
				URL:              pr.URL,
				OpenedAt:         pr.CreatedAt,
				ClosedAt:         pr.ClosedAt,
				MergedAt:         pr.MergedAt,
				LastSyncedAt:     syncedAt,
			}
		} else {
			sprintID, ok := parseSprintBranch(pr.HeadBranch)
			if !ok {
				continue
			}
			if _, sprintOK := sprints[sprintID]; !sprintOK {
				continue
			}
			snapshot = store.PullRequestSnapshot{
				GitHubPRNumber:   pr.GitHubPRNumber,
				GitHubPRNodeID:   pr.GitHubPRNodeID,
				PRKind:           "sprint",
				SprintID:         sprintID,
				TaskID:           nil,
				HeadBranch:       pr.HeadBranch,
				BaseBranch:       pr.BaseBranch,
				Status:           pr.State,
				AutoMergeEnabled: pr.AutoMergeEnabled,
				HeadSHA:          pr.HeadSHA,
				URL:              pr.URL,
				OpenedAt:         pr.CreatedAt,
				ClosedAt:         pr.ClosedAt,
				MergedAt:         pr.MergedAt,
				LastSyncedAt:     syncedAt,
			}
			if strings.TrimSpace(defaultBranch) != "" && pr.BaseBranch != defaultBranch && pr.State == "open" {
				return nil, nil, newValidationError(
					"open sprint pull request #%d targets %s instead of %s",
					pr.GitHubPRNumber,
					pr.BaseBranch,
					defaultBranch,
				)
			}
		}

		if _, exists := byNumber[snapshot.GitHubPRNumber]; exists {
			continue
		}
		projected = append(projected, snapshot)
		byNumber[snapshot.GitHubPRNumber] = snapshot
	}

	sort.Slice(projected, func(i, j int) bool {
		return projected[i].GitHubPRNumber < projected[j].GitHubPRNumber
	})
	return projected, byNumber, nil
}

func projectWorkflowRuns(
	runs []toolgithub.WorkflowRun,
	projectedPRs map[int]store.PullRequestSnapshot,
	sprints map[string]parsedSprint,
	tasks map[string]parsedTask,
	syncedAt string,
) []store.CIRunSnapshot {
	prByHeadSHA := make(map[string]store.PullRequestSnapshot)
	prByHeadBranch := make(map[string]store.PullRequestSnapshot)
	for _, pr := range projectedPRs {
		if strings.TrimSpace(pr.HeadSHA) != "" {
			prByHeadSHA[pr.HeadSHA] = pr
		}
		if strings.TrimSpace(pr.HeadBranch) != "" {
			prByHeadBranch[pr.HeadBranch] = pr
		}
	}

	projected := make([]store.CIRunSnapshot, 0)
	seen := make(map[int64]struct{}, len(runs))
	for _, run := range runs {
		if _, ok := seen[run.GitHubRunID]; ok {
			continue
		}
		seen[run.GitHubRunID] = struct{}{}

		var sprintID string
		var taskID *string
		var prNumber *int

		if branchSprintID, branchTaskID, ok := parseTaskBranch(run.HeadBranch); ok {
			if task, taskOK := tasks[branchTaskID]; taskOK {
				sprintID = task.Snapshot.SprintID
				taskID = optionalText(task.Snapshot.TaskID)
				if pr, ok := prByHeadSHA[run.HeadSHA]; ok {
					prNumber = intPtr(pr.GitHubPRNumber)
				} else if pr, ok := prByHeadBranch[run.HeadBranch]; ok {
					prNumber = intPtr(pr.GitHubPRNumber)
				}
			} else {
				_ = branchSprintID
				continue
			}
		} else if branchSprintID, ok := parseSprintBranch(run.HeadBranch); ok {
			if _, sprintOK := sprints[branchSprintID]; !sprintOK {
				continue
			}
			sprintID = branchSprintID
			if pr, ok := prByHeadSHA[run.HeadSHA]; ok {
				prNumber = intPtr(pr.GitHubPRNumber)
			} else if pr, ok := prByHeadBranch[run.HeadBranch]; ok {
				prNumber = intPtr(pr.GitHubPRNumber)
			}
		} else if pr, ok := prByHeadSHA[run.HeadSHA]; ok {
			sprintID = pr.SprintID
			taskID = pr.TaskID
			prNumber = intPtr(pr.GitHubPRNumber)
		} else {
			continue
		}

		if strings.TrimSpace(sprintID) == "" {
			continue
		}

		completedAt := ""
		if run.Status == "completed" || strings.TrimSpace(run.Conclusion) != "" {
			completedAt = run.UpdatedAt
		}

		workflowName := run.WorkflowName
		if strings.TrimSpace(workflowName) == "" {
			workflowName = run.Name
		}

		projected = append(projected, store.CIRunSnapshot{
			GitHubRunID:    run.GitHubRunID,
			SprintID:       sprintID,
			TaskID:         taskID,
			GitHubPRNumber: prNumber,
			WorkflowName:   workflowName,
			HeadSHA:        run.HeadSHA,
			Status:         run.Status,
			Conclusion:     run.Conclusion,
			HTMLURL:        run.URL,
			StartedAt:      run.StartedAt,
			CompletedAt:    completedAt,
			LastSyncedAt:   syncedAt,
		})
	}

	sort.Slice(projected, func(i, j int) bool {
		return projected[i].GitHubRunID < projected[j].GitHubRunID
	})
	return projected
}

func validateExecuteOptions(opts ExecuteOptions) error {
	if strings.TrimSpace(opts.WorkDir) == "" {
		return newValidationError("workdir is required")
	}
	if strings.TrimSpace(opts.Repo) == "" {
		return newValidationError("repo is required")
	}
	if strings.TrimSpace(opts.DefaultBranch) == "" {
		return newValidationError("default_branch is required")
	}
	return nil
}

func decodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return newValidationError("payload is required")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return newValidationError("decode payload: %v", err)
	}
	return nil
}

func asToolError(err error) *ToolError {
	if err == nil {
		return nil
	}

	var validationErr *validationError
	if errors.As(err, &validationErr) {
		code := ErrorCodeGitHubData
		if strings.Contains(validationErr.message, "payload") ||
			strings.Contains(validationErr.message, "unsupported op") ||
			strings.Contains(validationErr.message, "op is required") ||
			strings.Contains(validationErr.message, "workdir is required") ||
			strings.Contains(validationErr.message, "repo is required") ||
			strings.Contains(validationErr.message, "default_branch is required") ||
			strings.Contains(validationErr.message, "full_reconcile.reason") ||
			strings.Contains(validationErr.message, "ingest_webhook.") ||
			strings.Contains(validationErr.message, "reconcile_issue.") ||
			strings.Contains(validationErr.message, "reconcile_pull_request.") ||
			strings.Contains(validationErr.message, "reconcile_ci_run.") ||
			strings.Contains(validationErr.message, "header is required") ||
			strings.Contains(validationErr.message, "request method must be POST") {
			code = ErrorCodeInvalid
		}
		return &ToolError{
			Code:      code,
			Message:   err.Error(),
			Retryable: false,
		}
	}

	message := err.Error()
	code := ErrorCodeInternal
	retryable := true
	switch {
	case strings.Contains(message, "list ") || strings.Contains(message, "get "):
		code = ErrorCodeGitHubRead
	case strings.Contains(message, "apply github projection"):
		code = ErrorCodeProjection
		retryable = false
	default:
		retryable = false
	}

	return &ToolError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
}

func currentUTCTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func intPtr(value int) *int {
	return &value
}

func repoConfigSnapshot(repo, defaultBranch string) store.RepoConfigSnapshot {
	parts := strings.SplitN(strings.TrimSpace(repo), "/", 2)
	if len(parts) != 2 {
		return store.RepoConfigSnapshot{
			GitHubOwner:   strings.TrimSpace(repo),
			DefaultBranch: defaultBranch,
		}
	}
	return store.RepoConfigSnapshot{
		GitHubOwner:   strings.TrimSpace(parts[0]),
		GitHubRepo:    strings.TrimSpace(parts[1]),
		DefaultBranch: defaultBranch,
	}
}
