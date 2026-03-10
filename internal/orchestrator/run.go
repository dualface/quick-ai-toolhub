package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"quick-ai-toolhub/internal/agentrun"
	"quick-ai-toolhub/internal/store"
)

type reviewDecision string

type qaResultDecision string

const (
	reviewDecisionPass          reviewDecision = "pass"
	reviewDecisionRequestChange reviewDecision = "request_changes"
	reviewDecisionAwaitHuman    reviewDecision = "awaiting_human"

	qaResultAdvance qaResultDecision = "advance"
	qaResultRetry   qaResultDecision = "retry"
	qaResultBlocked qaResultDecision = "blocked"
)

type stageArtifactRefsSnapshot struct {
	Developer             agentrun.ArtifactRefs
	QA                    agentrun.ArtifactRefs
	Review                agentrun.ArtifactRefs
	LastStage             Stage
	LastStageArtifactRefs agentrun.ArtifactRefs
}

type stageArtifactEventRecord struct {
	EventType   string `bun:"event_type"`
	PayloadJSON string `bun:"payload_json"`
}

type stageResultArtifactEnvelope struct {
	StageResult *stageResultArtifactPayload `json:"stage_result"`
}

type stageResultArtifactPayload struct {
	Stage        Stage                 `json:"stage"`
	ArtifactRefs agentrun.ArtifactRefs `json:"artifact_refs"`
}

type stageWriteRejectedError struct {
	Current taskRuntimeSnapshot
	Reason  string
}

func (e *stageWriteRejectedError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Reason) == "" {
		return fmt.Sprintf("task %s stage write rejected", e.Current.TaskID)
	}
	return fmt.Sprintf("task %s stage write rejected: %s", e.Current.TaskID, strings.TrimSpace(e.Reason))
}

func (s *Service) loadTaskRuntime(ctx context.Context, taskID string) (taskRuntimeSnapshot, *store.SprintProjection, error) {
	base, err := s.store.BaseStore()
	if err != nil {
		return taskRuntimeSnapshot{}, nil, err
	}

	taskSnapshot, err := loadTaskRuntimeSnapshot(ctx, base.DB(), taskID)
	if err != nil {
		return taskRuntimeSnapshot{}, nil, err
	}
	sprintProjection, err := base.LoadSprintProjection(ctx, taskSnapshot.SprintID)
	if err != nil {
		return taskRuntimeSnapshot{}, nil, err
	}

	return taskSnapshot, &sprintProjection, nil
}

func loadTaskRuntimeSnapshot(ctx context.Context, db bun.IDB, taskID string) (taskRuntimeSnapshot, error) {
	var snapshot taskRuntimeSnapshot
	err := db.NewSelect().
		TableExpr("tasks").
		Column(
			"task_id",
			"sprint_id",
			"task_local_id",
			"sequence_no",
			"github_issue_number",
			"status",
			"attempt_total",
			"qa_fail_count",
			"review_fail_count",
			"ci_fail_count",
			"current_failure_fingerprint",
			"active_pr_number",
			"task_branch",
			"worktree_path",
			"needs_human",
			"human_reason",
		).
		Where("task_id = ?", strings.TrimSpace(taskID)).
		Limit(1).
		Scan(ctx, &snapshot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return taskRuntimeSnapshot{}, fmt.Errorf("task %s not found", strings.TrimSpace(taskID))
		}
		return taskRuntimeSnapshot{}, fmt.Errorf("load task runtime for %s: %w", strings.TrimSpace(taskID), err)
	}
	snapshot.Projection = snapshot.toProjection()
	return snapshot, nil
}

func (s *Service) loadLatestStageArtifactRefs(ctx context.Context, taskID string) (stageArtifactRefsSnapshot, error) {
	base, err := s.store.BaseStore()
	if err != nil {
		return stageArtifactRefsSnapshot{}, err
	}
	return loadLatestStageArtifactRefs(ctx, base.DB(), taskID)
}

func loadLatestStageArtifactRefs(ctx context.Context, db bun.IDB, taskID string) (stageArtifactRefsSnapshot, error) {
	var rows []stageArtifactEventRecord
	err := db.NewSelect().
		TableExpr("events").
		Column("event_type", "payload_json").
		Where("task_id = ?", strings.TrimSpace(taskID)).
		Where("event_type IN (?)", bun.List([]string{
			eventDeveloperCompleted,
			eventDeveloperBlocked,
			eventQAPassed,
			eventQAFailed,
			eventReviewAggregated,
			eventTaskBlocked,
			eventTaskEscalated,
		})).
		OrderExpr("rowid DESC").
		Scan(ctx, &rows)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return stageArtifactRefsSnapshot{}, nil
		}
		return stageArtifactRefsSnapshot{}, fmt.Errorf("load latest stage artifact refs for %s: %w", strings.TrimSpace(taskID), err)
	}

	var refs stageArtifactRefsSnapshot
	found := make(map[Stage]struct{}, 3)
	for _, row := range rows {
		stage, artifactRefs, ok, err := stageArtifactRefsForEvent(row)
		if err != nil {
			return stageArtifactRefsSnapshot{}, fmt.Errorf("decode %s stage artifact refs for %s: %w", strings.TrimSpace(row.EventType), strings.TrimSpace(taskID), err)
		}
		if !ok {
			continue
		}
		if refs.LastStage == "" {
			refs.LastStage = stage
			refs.LastStageArtifactRefs = artifactRefs
		}
		if _, exists := found[stage]; exists {
			continue
		}

		refs.set(stage, artifactRefs)
		found[stage] = struct{}{}
		if len(found) == 3 {
			break
		}
	}
	return refs, nil
}

func (t taskRuntimeSnapshot) toProjection() store.TaskProjection {
	return store.TaskProjection{
		TaskID:            t.TaskID,
		SprintID:          t.SprintID,
		TaskLocalID:       t.TaskLocalID,
		SequenceNo:        t.SequenceNo,
		GitHubIssueNumber: t.GitHubIssueNumber,
		Status:            t.Status,
		ActivePRNumber:    t.ActivePRNumber,
		TaskBranch:        t.TaskBranch,
		WorktreePath:      t.WorktreePath,
		NeedsHuman:        t.NeedsHuman,
		HumanReason:       t.HumanReason,
	}
}

func rejectStageWrite(current taskRuntimeSnapshot, reason string) error {
	return &stageWriteRejectedError{
		Current: current,
		Reason:  strings.TrimSpace(reason),
	}
}

func ensureStageWriteAllowed(current taskRuntimeSnapshot, expectedStatus string, expectedAttempt int) error {
	expectedStatus = strings.TrimSpace(expectedStatus)
	if expectedStatus != "" && strings.TrimSpace(current.Status) != expectedStatus {
		return rejectStageWrite(current, fmt.Sprintf("expected status %s, found %s", expectedStatus, strings.TrimSpace(current.Status)))
	}
	if expectedAttempt > 0 && current.AttemptTotal != expectedAttempt {
		return rejectStageWrite(current, fmt.Sprintf("expected attempt %d, found %d", expectedAttempt, current.AttemptTotal))
	}
	return nil
}

func (s *Service) enterDeveloperStage(ctx context.Context, snapshot taskRuntimeSnapshot, retry bool) (taskRuntimeSnapshot, error) {
	targetAttempt := developerAttempt(snapshot)
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, snapshot.Status, snapshot.AttemptTotal); err != nil {
			return err
		}

		if retry {
			if _, err := tx.AppendEvent(ctx, store.AppendEventPayload{
				EventID:        taskEventID(current.TaskID, eventRetryApproved, targetAttempt),
				EntityType:     "task",
				EntityID:       current.TaskID,
				SprintID:       stringPointer(current.SprintID),
				TaskID:         stringPointer(current.TaskID),
				EventType:      eventRetryApproved,
				Source:         orchestratorSource,
				Attempt:        intPointer(targetAttempt),
				IdempotencyKey: taskEventIDKey(current.TaskID, eventRetryApproved, targetAttempt),
				PayloadJSON: map[string]any{
					"task_status_from":    strings.TrimSpace(current.Status),
					"task_status_to":      "dev_in_progress",
					"attempt_total":       targetAttempt,
					"failure_fingerprint": optionalString(current.CurrentFailureFingerprint),
					"retry_origin_status": strings.TrimSpace(current.Status),
				},
				OccurredAt: currentUTCTimestamp(),
			}); err != nil {
				return err
			}
		}

		if _, err := tx.AppendEvent(ctx, store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventDeveloperStarted, targetAttempt),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventDeveloperStarted,
			Source:         orchestratorSource,
			Attempt:        intPointer(targetAttempt),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventDeveloperStarted, targetAttempt),
			PayloadJSON: map[string]any{
				"task_status_from": strings.TrimSpace(current.Status),
				"task_status_to":   "dev_in_progress",
				"attempt_total":    targetAttempt,
				"worktree_path":    optionalString(current.WorktreePath),
			},
			OccurredAt: currentUTCTimestamp(),
		}); err != nil {
			return err
		}

		update := store.UpdateTaskStatePayload{
			TaskID:       current.TaskID,
			Status:       "dev_in_progress",
			AttemptTotal: intPointer(targetAttempt),
		}
		if shouldUpdateTaskState(current, update) {
			if _, err := tx.UpdateTaskState(ctx, update); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}
	return loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
}

func decodeStageArtifactPayload(payloadJSON string) (stageResultArtifactPayload, error) {
	var payload stageResultArtifactEnvelope
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return stageResultArtifactPayload{}, err
	}
	if payload.StageResult == nil {
		return stageResultArtifactPayload{}, nil
	}
	return *payload.StageResult, nil
}

func stageArtifactRefsForEvent(row stageArtifactEventRecord) (Stage, agentrun.ArtifactRefs, bool, error) {
	stage, ok := stageForEventType(row.EventType)
	switch strings.TrimSpace(row.EventType) {
	case eventTaskBlocked, eventTaskEscalated:
	default:
		if !ok {
			return "", agentrun.ArtifactRefs{}, false, nil
		}
	}

	payload, err := decodeStageArtifactPayload(row.PayloadJSON)
	if err != nil {
		return "", agentrun.ArtifactRefs{}, false, err
	}

	if strings.TrimSpace(row.EventType) == eventTaskBlocked || strings.TrimSpace(row.EventType) == eventTaskEscalated {
		stage = payload.Stage
	}
	if stage == "" {
		return "", agentrun.ArtifactRefs{}, false, nil
	}
	return stage, payload.ArtifactRefs, true, nil
}

func stageForEventType(eventType string) (Stage, bool) {
	switch strings.TrimSpace(eventType) {
	case eventDeveloperCompleted, eventDeveloperBlocked:
		return StageDeveloper, true
	case eventQAPassed, eventQAFailed:
		return StageQA, true
	case eventReviewAggregated:
		return StageReview, true
	default:
		return "", false
	}
}

func (s *stageArtifactRefsSnapshot) set(stage Stage, refs agentrun.ArtifactRefs) {
	switch stage {
	case StageDeveloper:
		s.Developer = refs
	case StageQA:
		s.QA = refs
	case StageReview:
		s.Review = refs
	}
}

func (s stageArtifactRefsSnapshot) refsFor(stage Stage) agentrun.ArtifactRefs {
	switch stage {
	case StageDeveloper:
		return s.Developer
	case StageQA:
		return s.QA
	case StageReview:
		return s.Review
	default:
		return agentrun.ArtifactRefs{}
	}
}

func (s *Service) markReviewStarted(ctx context.Context, snapshot taskRuntimeSnapshot) (taskRuntimeSnapshot, error) {
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, snapshot.Status, snapshot.AttemptTotal); err != nil {
			return err
		}

		_, err = tx.AppendEvent(ctx, store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventReviewStarted, current.AttemptTotal),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventReviewStarted,
			Source:         orchestratorSource,
			Attempt:        intPointer(current.AttemptTotal),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventReviewStarted, current.AttemptTotal),
			PayloadJSON: map[string]any{
				"task_status_from": "review_in_progress",
				"task_status_to":   "review_in_progress",
			},
			OccurredAt: currentUTCTimestamp(),
		})
		return err
	})
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}
	return loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
}

func (s *Service) runAgentStage(
	ctx context.Context,
	stage Stage,
	snapshot taskRuntimeSnapshot,
	opts RunTaskOptions,
	qaArtifactRefs agentrun.ArtifactRefs,
	reviewerArtifactRefs agentrun.ArtifactRefs,
	artifactRefs agentrun.ArtifactRefs,
	lens string,
) (StageResult, error) {
	worktreePath := strings.TrimSpace(optionalString(snapshot.WorktreePath))
	if worktreePath == "" {
		return StageResult{}, fmt.Errorf("task %s has no worktree_path", snapshot.TaskID)
	}

	agentType := agentTypeForStage(stage)
	req := agentrun.Request{
		AgentType:  agentType,
		TaskID:     snapshot.TaskID,
		Attempt:    snapshot.AttemptTotal,
		Lens:       strings.TrimSpace(lens),
		ConfigFile: opts.ConfigFile,
		ContextRefs: agentrun.ContextRefs{
			SprintID:             snapshot.SprintID,
			WorktreePath:         worktreePath,
			ArtifactRefs:         artifactRefs,
			QAArtifactRefs:       qaArtifactRefs,
			ReviewerArtifactRefs: reviewerArtifactRefs,
		},
	}

	resp := s.agentRunner.Execute(ctx, req, agentrun.ExecuteOptions{
		PlanFile:          opts.PlanFile,
		TasksDir:          opts.TasksDir,
		WorkDir:           worktreePath,
		OutputRoot:        opts.OutputRoot,
		Timeout:           opts.Timeout,
		Yolo:              opts.Yolo,
		IsolatedCodexHome: opts.IsolatedCodexHome,
	})
	if !resp.OK {
		if resp.Error != nil {
			return StageResult{}, fmt.Errorf("run %s agent for %s: %s", stage, snapshot.TaskID, resp.Error.Message)
		}
		return StageResult{}, fmt.Errorf("run %s agent for %s: unknown failure", stage, snapshot.TaskID)
	}
	if resp.Data == nil {
		return StageResult{}, fmt.Errorf("run %s agent for %s: empty response payload", stage, snapshot.TaskID)
	}

	return StageResult{
		Stage:              stage,
		AgentType:          agentType,
		Attempt:            snapshot.AttemptTotal,
		Status:             resp.Data.Status,
		Summary:            resp.Data.Summary,
		NextAction:         resp.Data.NextAction,
		FailureFingerprint: strings.TrimSpace(resp.Data.FailureFingerprint),
		ArtifactRefs:       resp.Data.ArtifactRefs,
		Findings:           copyFindings(resp.Data.Findings),
	}, nil
}

func (s *Service) runReviewStage(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	opts RunTaskOptions,
	lens string,
	artifactRefs agentrun.ArtifactRefs,
) (StageResult, []agentrun.Finding, error) {
	result, err := s.runAgentStage(ctx, StageReview, snapshot, opts, agentrun.ArtifactRefs{}, agentrun.ArtifactRefs{}, artifactRefs, lens)
	if err != nil {
		return StageResult{}, nil, err
	}
	if isRetryableStageFailure(result) {
		result.Stage = StageReview
		result.AgentType = agentrun.AgentReviewer
		result.Attempt = snapshot.AttemptTotal
		return result, nil, nil
	}

	normalizedFindings, err := normalizeFindings(result.Findings, lens)
	if err != nil {
		return *retryableMalformedReviewResult(snapshot.AttemptTotal, result.ArtifactRefs, err), nil, nil
	}
	if err := validateReviewObservationContract(result, normalizedFindings, lens); err != nil {
		return *retryableMalformedReviewResult(snapshot.AttemptTotal, result.ArtifactRefs, err), nil, nil
	}

	dedupedFindings := dedupePersistedReviewFindings(normalizedFindings)
	result.Findings = copyFindings(dedupedFindings)
	result.Stage = StageReview
	result.AgentType = agentrun.AgentReviewer
	result.Attempt = snapshot.AttemptTotal
	return result, copyFindings(dedupedFindings), nil
}

func retryableMalformedReviewResult(attempt int, refs agentrun.ArtifactRefs, err error) *StageResult {
	return &StageResult{
		Stage:              StageReview,
		AgentType:          agentrun.AgentReviewer,
		Attempt:            attempt,
		Status:             "failed",
		Summary:            fmt.Sprintf("review output rejected: %s", err),
		NextAction:         "retry",
		FailureFingerprint: agentrun.ErrorCodeMalformedOutput,
		ArtifactRefs:       refs,
	}
}

func (s *Service) handleDeveloperResult(ctx context.Context, snapshot taskRuntimeSnapshot, result StageResult) (StageResult, taskRuntimeSnapshot, error) {
	if isRetryableStageFailure(result) {
		return retryableStageResult(snapshot, result), snapshot, nil
	}

	if developerResultAdvances(result) {
		update := store.UpdateTaskStatePayload{
			TaskID:                    snapshot.TaskID,
			Status:                    "qa_in_progress",
			CurrentFailureFingerprint: emptyStringPointer(),
		}
		eventID, updatedSnapshot, err := s.recordStageOutcome(ctx, snapshot, "dev_in_progress", eventDeveloperCompleted, "qa_in_progress", result, update)
		if err != nil {
			return StageResult{}, taskRuntimeSnapshot{}, err
		}
		result.EventID = eventID
		result.TaskStatus = updatedSnapshot.Status
		return result, updatedSnapshot, nil
	}

	update := store.UpdateTaskStatePayload{
		TaskID:      snapshot.TaskID,
		Status:      "blocked",
		NeedsHuman:  boolPointer(true),
		HumanReason: stringPointer(blockedHumanReason(result)),
	}
	if strings.TrimSpace(result.FailureFingerprint) == "" {
		fingerprint := stageFailureFingerprint(StageDeveloper, result)
		update.CurrentFailureFingerprint = stringPointer(fingerprint)
		result.FailureFingerprint = fingerprint
	}
	eventID, updatedSnapshot, err := s.recordStageOutcome(ctx, snapshot, "dev_in_progress", eventDeveloperBlocked, "blocked", result, update)
	if err != nil {
		return StageResult{}, taskRuntimeSnapshot{}, err
	}
	result.EventID = eventID
	result.TaskStatus = updatedSnapshot.Status
	return result, updatedSnapshot, nil
}

func (s *Service) handleQAResult(ctx context.Context, snapshot taskRuntimeSnapshot, result StageResult) (StageResult, taskRuntimeSnapshot, error) {
	if isRetryableStageFailure(result) {
		return retryableStageResult(snapshot, result), snapshot, nil
	}

	switch decideQAResult(result) {
	case qaResultAdvance:
		update := store.UpdateTaskStatePayload{
			TaskID:                    snapshot.TaskID,
			Status:                    "review_in_progress",
			QAFailCount:               intPointer(0),
			CurrentFailureFingerprint: emptyStringPointer(),
			NeedsHuman:                boolPointer(false),
			HumanReason:               emptyStringPointer(),
		}
		eventID, updatedSnapshot, err := s.recordStageOutcome(ctx, snapshot, "qa_in_progress", eventQAPassed, "review_in_progress", result, update)
		if err != nil {
			return StageResult{}, taskRuntimeSnapshot{}, err
		}
		result.EventID = eventID
		result.TaskStatus = updatedSnapshot.Status
		return result, updatedSnapshot, nil

	case qaResultBlocked:
		if strings.TrimSpace(result.FailureFingerprint) == "" {
			result.FailureFingerprint = stageFailureFingerprint(StageQA, result)
		}
		return s.recordTaskBlocked(ctx, snapshot, StageQA, result)
	}

	update := store.UpdateTaskStatePayload{
		TaskID:                    snapshot.TaskID,
		Status:                    "qa_failed",
		QAFailCount:               intPointer(snapshot.QAFailCount + 1),
		NeedsHuman:                boolPointer(false),
		HumanReason:               emptyStringPointer(),
		CurrentFailureFingerprint: stringPointer(stageFailureFingerprint(StageQA, result)),
	}
	result.FailureFingerprint = optionalString(update.CurrentFailureFingerprint)

	eventID, updatedSnapshot, err := s.recordStageOutcome(ctx, snapshot, "qa_in_progress", eventQAFailed, "qa_failed", result, update)
	if err != nil {
		return StageResult{}, taskRuntimeSnapshot{}, err
	}
	result.EventID = eventID
	result.TaskStatus = updatedSnapshot.Status
	return result, updatedSnapshot, nil
}

func (s *Service) handleReviewResult(ctx context.Context, snapshot taskRuntimeSnapshot, result StageResult, rawFindings []agentrun.Finding) (StageResult, taskRuntimeSnapshot, error) {
	if isRetryableStageFailure(result) {
		return retryableStageResult(snapshot, result), snapshot, nil
	}

	decision := classifyReviewResult(result)
	targetStatus := "review_failed"
	update := store.UpdateTaskStatePayload{
		TaskID:          snapshot.TaskID,
		Status:          targetStatus,
		ReviewFailCount: intPointer(snapshot.ReviewFailCount + 1),
		NeedsHuman:      boolPointer(false),
		HumanReason:     emptyStringPointer(),
	}

	switch decision {
	case reviewDecisionPass:
		targetStatus = "pr_open"
		update.Status = targetStatus
		update.ReviewFailCount = intPointer(0)
		update.CurrentFailureFingerprint = emptyStringPointer()
	case reviewDecisionAwaitHuman:
		if strings.TrimSpace(result.FailureFingerprint) == "" {
			result.FailureFingerprint = reviewFailureFingerprint(snapshot.AttemptTotal, result.Findings, "awaiting_human")
		}
		update.CurrentFailureFingerprint = stringPointer(result.FailureFingerprint)
	default:
		if strings.TrimSpace(result.FailureFingerprint) == "" {
			result.FailureFingerprint = reviewFailureFingerprint(snapshot.AttemptTotal, result.Findings, "request_changes")
		}
		update.CurrentFailureFingerprint = stringPointer(result.FailureFingerprint)
	}

	eventID, updatedSnapshot, err := s.recordReviewOutcome(ctx, snapshot, "review_in_progress", targetStatus, decision, result, rawFindings, update)
	if err != nil {
		return StageResult{}, taskRuntimeSnapshot{}, err
	}

	if decision == reviewDecisionAwaitHuman {
		updatedSnapshot, err = s.recordTaskAwaitingHuman(ctx, updatedSnapshot, result.Summary, result.FailureFingerprint)
		if err != nil {
			return StageResult{}, taskRuntimeSnapshot{}, err
		}
	}

	result.EventID = eventID
	result.TaskStatus = updatedSnapshot.Status
	return result, updatedSnapshot, nil
}

func (s *Service) handleTransitionBudgetExceeded(
	ctx context.Context,
	taskID string,
	maxTransitions int,
	stageResults []StageResult,
	resumeRefs stageArtifactRefsSnapshot,
) (RunTaskResult, error) {
	taskSnapshot, sprintProjection, err := s.loadTaskRuntime(ctx, taskID)
	if err != nil {
		return RunTaskResult{}, err
	}

	switch strings.TrimSpace(taskSnapshot.Status) {
	case "pr_open", "blocked", "escalated", "awaiting_human", "done", "canceled":
		return buildNoOpRunTaskResult(taskSnapshot, sprintProjection, stageResults, resumeRefs), nil
	}

	previousStatus := strings.TrimSpace(taskSnapshot.Status)
	summary := fmt.Sprintf("task exceeded max stage transitions (%d)", maxTransitions)
	if previousStatus != "" {
		summary = fmt.Sprintf("%s while in %s", summary, previousStatus)
	}
	failureFingerprint := transitionBudgetFailureFingerprint(taskSnapshot, stageResults)
	escalationStageResult, hasEscalationStageResult := transitionBudgetStageResult(taskSnapshot, stageResults, resumeRefs, failureFingerprint)

	taskSnapshot, err = s.recordTaskEscalated(
		ctx,
		taskSnapshot,
		summary,
		failureFingerprint,
		maxTransitions,
		escalationStageResult,
		hasEscalationStageResult,
	)
	if err != nil {
		if result, handled, handleErr := s.handleStageWriteRejected(ctx, taskID, stageResults, resumeRefs, err); handled {
			return result, handleErr
		}
		return RunTaskResult{}, err
	}

	result := buildNoOpRunTaskResult(taskSnapshot, sprintProjection, stageResults, resumeRefs)
	if len(stageResults) > 0 {
		last := stageResults[len(stageResults)-1]
		result.Stage = last.Stage
		result.ArtifactRefs = firstArtifactRefs(last.ArtifactRefs, result.ArtifactRefs)
	} else {
		result.Stage = resumeStageForTaskStatus(previousStatus, resumeRefs)
	}
	result.Summary = summary
	result.FailureFingerprint = failureFingerprint
	result.NextAction = nextActionForTaskStatus(taskSnapshot.Status)
	return result, nil
}

func (s *Service) recordStageOutcome(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	expectedSourceStatus string,
	eventType string,
	targetStatus string,
	result StageResult,
	update store.UpdateTaskStatePayload,
) (string, taskRuntimeSnapshot, error) {
	var eventID string
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, expectedSourceStatus, snapshot.AttemptTotal); err != nil {
			return err
		}

		payload := store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventType, current.AttemptTotal),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventType,
			Source:         orchestratorSource,
			Attempt:        intPointer(current.AttemptTotal),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventType, current.AttemptTotal),
			PayloadJSON: map[string]any{
				"task_status_from": strings.TrimSpace(current.Status),
				"task_status_to":   targetStatus,
				"stage_result":     result,
			},
			OccurredAt: currentUTCTimestamp(),
		}

		appended, err := tx.AppendEvent(ctx, payload)
		if err != nil {
			return err
		}
		eventID = appended.EventID

		if shouldUpdateTaskState(current, update) {
			if _, err := tx.UpdateTaskState(ctx, update); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}
	updatedSnapshot, err := loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}
	return eventID, updatedSnapshot, nil
}

func (s *Service) recordTaskBlocked(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	stage Stage,
	result StageResult,
) (StageResult, taskRuntimeSnapshot, error) {
	update := store.UpdateTaskStatePayload{
		TaskID:                    snapshot.TaskID,
		Status:                    "blocked",
		CurrentFailureFingerprint: stringPointer(result.FailureFingerprint),
		NeedsHuman:                boolPointer(true),
		HumanReason:               stringPointer(blockedHumanReason(result)),
	}

	eventID, updatedSnapshot, err := s.recordStageOutcome(ctx, snapshot, snapshot.Status, eventTaskBlocked, "blocked", result, update)
	if err != nil {
		return StageResult{}, taskRuntimeSnapshot{}, err
	}
	result.Stage = stage
	result.EventID = eventID
	result.TaskStatus = updatedSnapshot.Status
	return result, updatedSnapshot, nil
}

func (s *Service) recordReviewOutcome(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	expectedSourceStatus string,
	targetStatus string,
	decision reviewDecision,
	result StageResult,
	rawFindings []agentrun.Finding,
	update store.UpdateTaskStatePayload,
) (string, taskRuntimeSnapshot, error) {
	var eventID string
	persistedFindings := copyFindings(rawFindings)
	if len(persistedFindings) == 0 {
		persistedFindings = copyFindings(result.Findings)
	}
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, expectedSourceStatus, snapshot.AttemptTotal); err != nil {
			return err
		}

		payload := store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventReviewAggregated, current.AttemptTotal),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventReviewAggregated,
			Source:         orchestratorSource,
			Attempt:        intPointer(current.AttemptTotal),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventReviewAggregated, current.AttemptTotal),
			PayloadJSON: map[string]any{
				"task_status_from": strings.TrimSpace(current.Status),
				"task_status_to":   targetStatus,
				"decision":         string(decision),
				"stage_result":     result,
			},
			OccurredAt: currentUTCTimestamp(),
		}

		appended, err := tx.AppendEvent(ctx, payload)
		if err != nil {
			return err
		}
		eventID = appended.EventID

		if err := persistReviewFindings(ctx, tx.DB(), current.TaskID, eventID, persistedFindings); err != nil {
			return err
		}
		if shouldUpdateTaskState(current, update) {
			if _, err := tx.UpdateTaskState(ctx, update); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}
	updatedSnapshot, err := loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
	if err != nil {
		return "", taskRuntimeSnapshot{}, err
	}
	return eventID, updatedSnapshot, nil
}

func (s *Service) recordTaskAwaitingHuman(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	summary string,
	failureFingerprint string,
) (taskRuntimeSnapshot, error) {
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, snapshot.Status, snapshot.AttemptTotal); err != nil {
			return err
		}

		_, err = tx.AppendEvent(ctx, store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventTaskAwaitingHuman, current.AttemptTotal),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventTaskAwaitingHuman,
			Source:         orchestratorSource,
			Attempt:        intPointer(current.AttemptTotal),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventTaskAwaitingHuman, current.AttemptTotal),
			PayloadJSON: map[string]any{
				"task_status_from":    strings.TrimSpace(current.Status),
				"task_status_to":      "awaiting_human",
				"summary":             strings.TrimSpace(summary),
				"failure_fingerprint": strings.TrimSpace(failureFingerprint),
			},
			OccurredAt: currentUTCTimestamp(),
		})
		if err != nil {
			return err
		}

		update := store.UpdateTaskStatePayload{
			TaskID:                    current.TaskID,
			Status:                    "awaiting_human",
			CurrentFailureFingerprint: stringPointer(failureFingerprint),
			NeedsHuman:                boolPointer(true),
			HumanReason:               stringPointer(summary),
		}
		if shouldUpdateTaskState(current, update) {
			if _, err := tx.UpdateTaskState(ctx, update); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}
	return loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
}

func (s *Service) recordTaskEscalated(
	ctx context.Context,
	snapshot taskRuntimeSnapshot,
	summary string,
	failureFingerprint string,
	maxTransitions int,
	stageResult StageResult,
	hasStageResult bool,
) (taskRuntimeSnapshot, error) {
	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		current, err := loadTaskRuntimeSnapshot(ctx, tx.DB(), snapshot.TaskID)
		if err != nil {
			return err
		}
		if err := ensureStageWriteAllowed(current, snapshot.Status, snapshot.AttemptTotal); err != nil {
			return err
		}

		payloadJSON := map[string]any{
			"task_status_from":      strings.TrimSpace(current.Status),
			"task_status_to":        "escalated",
			"summary":               strings.TrimSpace(summary),
			"failure_fingerprint":   strings.TrimSpace(failureFingerprint),
			"max_stage_transitions": maxTransitions,
		}
		if hasStageResult && shouldPersistEscalationStageResult(stageResult) {
			copiedStageResult := stageResult
			copiedStageResult.Findings = copyFindings(stageResult.Findings)
			payloadJSON["stage_result"] = copiedStageResult
		}

		_, err = tx.AppendEvent(ctx, store.AppendEventPayload{
			EventID:        taskEventID(current.TaskID, eventTaskEscalated, current.AttemptTotal),
			EntityType:     "task",
			EntityID:       current.TaskID,
			SprintID:       stringPointer(current.SprintID),
			TaskID:         stringPointer(current.TaskID),
			EventType:      eventTaskEscalated,
			Source:         orchestratorSource,
			Attempt:        intPointer(current.AttemptTotal),
			IdempotencyKey: taskEventIDKey(current.TaskID, eventTaskEscalated, current.AttemptTotal),
			PayloadJSON:    payloadJSON,
			OccurredAt:     currentUTCTimestamp(),
		})
		if err != nil {
			return err
		}

		update := store.UpdateTaskStatePayload{
			TaskID:                    current.TaskID,
			Status:                    "escalated",
			CurrentFailureFingerprint: stringPointer(failureFingerprint),
			NeedsHuman:                boolPointer(true),
			HumanReason:               stringPointer(summary),
		}
		if shouldUpdateTaskState(current, update) {
			if _, err := tx.UpdateTaskState(ctx, update); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}

	base, err := s.store.BaseStore()
	if err != nil {
		return taskRuntimeSnapshot{}, err
	}
	return loadTaskRuntimeSnapshot(ctx, base.DB(), snapshot.TaskID)
}

func persistReviewFindings(ctx context.Context, db bun.IDB, taskID, reviewEventID string, findings []agentrun.Finding) error {
	for _, finding := range dedupePersistedReviewFindings(findings) {
		normalizedFinding, err := normalizeReviewFinding(finding, "")
		if err != nil {
			return fmt.Errorf("validate review finding %q: %w", strings.TrimSpace(finding.FindingFingerprint), err)
		}

		fileRefsJSON, err := json.Marshal(normalizedFinding.FileRefs)
		if err != nil {
			return fmt.Errorf("marshal review finding file refs for %s: %w", normalizedFinding.FindingFingerprint, err)
		}

		_, err = db.ExecContext(ctx, `
			INSERT INTO review_findings (
				finding_id,
				task_id,
				review_event_id,
				reviewer_id,
				lens,
				severity,
				confidence,
				category,
				file_refs_json,
				summary,
				evidence,
				finding_fingerprint,
				suggested_action,
				aggregate_status,
				created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)
			ON CONFLICT(task_id, review_event_id, reviewer_id, finding_fingerprint) DO NOTHING
		`,
			reviewFindingID(taskID, reviewEventID, normalizedFinding.ReviewerID, normalizedFinding.FindingFingerprint),
			taskID,
			reviewEventID,
			normalizedFinding.ReviewerID,
			normalizedFinding.Lens,
			normalizedFinding.Severity,
			normalizedFinding.Confidence,
			normalizedFinding.Category,
			string(fileRefsJSON),
			normalizedFinding.Summary,
			normalizedFinding.Evidence,
			normalizedFinding.FindingFingerprint,
			normalizedFinding.SuggestedAction,
			currentUTCTimestamp(),
		)
		if err != nil {
			return fmt.Errorf("insert review finding %s: %w", normalizedFinding.FindingFingerprint, err)
		}
	}
	return nil
}

func dedupePersistedReviewFindings(findings []agentrun.Finding) []agentrun.Finding {
	indexByKey := make(map[string]int, len(findings))
	deduped := make([]agentrun.Finding, 0, len(findings))
	for _, finding := range findings {
		key := strings.TrimSpace(finding.ReviewerID) + "|" + reviewFindingAggregateKey(finding)
		index, exists := indexByKey[key]
		if exists {
			merged := deduped[index]
			merged.Severity = strongerReviewSeverity(merged.Severity, finding.Severity)
			merged.Confidence = strongerReviewConfidence(merged.Confidence, finding.Confidence, 1)
			merged.FileRefs = dedupeOrderedStrings(append(append([]string(nil), merged.FileRefs...), finding.FileRefs...))
			deduped[index] = merged
			continue
		}
		indexByKey[key] = len(deduped)
		deduped = append(deduped, finding)
	}
	return deduped
}

func buildRunTaskResult(
	task taskRuntimeSnapshot,
	sprint *store.SprintProjection,
	stageResults []StageResult,
	lastStage StageResult,
) RunTaskResult {
	result := RunTaskResult{
		TaskID:       task.TaskID,
		SprintID:     task.SprintID,
		Stage:        lastStage.Stage,
		Status:       task.Status,
		Summary:      lastStage.Summary,
		Attempt:      task.AttemptTotal,
		ArtifactRefs: lastStage.ArtifactRefs,
		NextAction:   nextActionForTaskStatus(task.Status),
		Task:         copyTaskProjection(task.toProjection()),
		Sprint:       copySprintProjection(sprint),
		StageResults: copyStageResults(stageResults),
	}
	result.FailureFingerprint = resolvedFailureFingerprint(task.Status, lastStage.FailureFingerprint, task.CurrentFailureFingerprint)
	return result
}

func buildNoOpRunTaskResult(
	task taskRuntimeSnapshot,
	sprint *store.SprintProjection,
	stageResults []StageResult,
	resumeRefs stageArtifactRefsSnapshot,
) RunTaskResult {
	result := RunTaskResult{
		TaskID:       task.TaskID,
		SprintID:     task.SprintID,
		Status:       task.Status,
		Summary:      summaryForTaskStatus(task.Status),
		Attempt:      task.AttemptTotal,
		NextAction:   nextActionForTaskStatus(task.Status),
		Task:         copyTaskProjection(task.toProjection()),
		Sprint:       copySprintProjection(sprint),
		StageResults: copyStageResults(stageResults),
	}
	if len(stageResults) > 0 {
		last := stageResults[len(stageResults)-1]
		result.Stage = last.Stage
		result.ArtifactRefs = last.ArtifactRefs
		result.FailureFingerprint = resolvedFailureFingerprint(task.Status, last.FailureFingerprint, task.CurrentFailureFingerprint)
	} else {
		result.Stage = resumeStageForTaskStatus(task.Status, resumeRefs)
		result.ArtifactRefs = artifactRefsForTaskStatus(task.Status, resumeRefs)
		result.FailureFingerprint = resolvedFailureFingerprint(task.Status, "", task.CurrentFailureFingerprint)
	}
	return result
}

func resolvedFailureFingerprint(taskStatus string, stageFailureFingerprint string, taskFailureFingerprint *string) string {
	stageFailureFingerprint = strings.TrimSpace(stageFailureFingerprint)
	if stageFailureFingerprint != "" {
		return stageFailureFingerprint
	}
	if !shouldExposeTaskFailureFingerprint(taskStatus) {
		return ""
	}
	return optionalString(taskFailureFingerprint)
}

func shouldExposeTaskFailureFingerprint(taskStatus string) bool {
	switch strings.TrimSpace(taskStatus) {
	case "qa_failed", "review_failed", "ci_failed", "merge_failed", "awaiting_human", "blocked", "escalated":
		return true
	default:
		return false
	}
}

func shouldUpdateTaskState(current taskRuntimeSnapshot, update store.UpdateTaskStatePayload) bool {
	if strings.TrimSpace(current.Status) != strings.TrimSpace(update.Status) {
		return true
	}
	if update.AttemptTotal != nil && current.AttemptTotal != *update.AttemptTotal {
		return true
	}
	if update.QAFailCount != nil && current.QAFailCount != *update.QAFailCount {
		return true
	}
	if update.ReviewFailCount != nil && current.ReviewFailCount != *update.ReviewFailCount {
		return true
	}
	if update.CIFailCount != nil && current.CIFailCount != *update.CIFailCount {
		return true
	}
	if update.CurrentFailureFingerprint != nil && optionalString(current.CurrentFailureFingerprint) != optionalString(update.CurrentFailureFingerprint) {
		return true
	}
	if update.ActivePRNumber != nil {
		if current.ActivePRNumber == nil || *current.ActivePRNumber != *update.ActivePRNumber {
			return true
		}
	}
	if update.TaskBranch != nil && optionalString(current.TaskBranch) != optionalString(update.TaskBranch) {
		return true
	}
	if update.WorktreePath != nil && optionalString(current.WorktreePath) != optionalString(update.WorktreePath) {
		return true
	}
	if update.NeedsHuman != nil && current.NeedsHuman != *update.NeedsHuman {
		return true
	}
	if update.HumanReason != nil && optionalString(current.HumanReason) != optionalString(update.HumanReason) {
		return true
	}
	return false
}

func agentTypeForStage(stage Stage) agentrun.AgentType {
	switch stage {
	case StageDeveloper:
		return agentrun.AgentDeveloper
	case StageQA:
		return agentrun.AgentQA
	default:
		return agentrun.AgentReviewer
	}
}

func developerAttempt(snapshot taskRuntimeSnapshot) int {
	switch strings.TrimSpace(snapshot.Status) {
	case "qa_failed", "review_failed":
		if snapshot.AttemptTotal <= 0 {
			return 1
		}
		return snapshot.AttemptTotal + 1
	default:
		if snapshot.AttemptTotal <= 0 {
			return 1
		}
		return snapshot.AttemptTotal
	}
}

func isPassingResult(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "completed", "completed_with_findings", "pass", "pass_with_findings", "passed", "passed_with_warning":
		return true
	default:
		return false
	}
}

func developerResultAdvances(result StageResult) bool {
	return isPassingResult(result.Status) && nextActionAdvancesStage(StageDeveloper, result.NextAction)
}

func decideQAResult(result StageResult) qaResultDecision {
	if nextActionRequestsHuman(result.NextAction) || nextActionBlocks(result.NextAction) {
		return qaResultBlocked
	}
	if isPassingResult(result.Status) && nextActionAdvancesStage(StageQA, result.NextAction) {
		return qaResultAdvance
	}
	return qaResultRetry
}

func nextActionAdvancesStage(stage Stage, nextAction string) bool {
	switch normalizeNextAction(nextAction) {
	case "proceed", "continue", "next", "advance":
		return true
	case "qa", "run_qa", "start_qa", "enter_qa", "to_qa":
		return stage == StageDeveloper
	case "review", "run_review", "start_review", "enter_review", "to_review":
		return stage == StageQA
	case "open_task_pr":
		return stage == StageReview
	default:
		return false
	}
}

func nextActionRequestsHuman(nextAction string) bool {
	return strings.Contains(normalizeNextAction(nextAction), "human")
}

func nextActionBlocks(nextAction string) bool {
	switch normalizeNextAction(nextAction) {
	case "block", "blocked", "stop", "escalate", "escalated":
		return true
	default:
		return false
	}
}

func normalizeNextAction(nextAction string) string {
	return strings.ToLower(strings.TrimSpace(nextAction))
}

func isRetryableStageFailure(result StageResult) bool {
	if strings.EqualFold(strings.TrimSpace(result.NextAction), "retry") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(result.Status), "timeout") {
		return true
	}

	switch strings.TrimSpace(result.FailureFingerprint) {
	case agentrun.ErrorCodeMalformedOutput, agentrun.ErrorCodeRunnerExecution, agentrun.ErrorCodeRunnerTimeout:
		return true
	default:
		return false
	}
}

func retryableStageResult(snapshot taskRuntimeSnapshot, result StageResult) StageResult {
	if strings.TrimSpace(result.FailureFingerprint) == "" {
		result.FailureFingerprint = stageFailureFingerprint(result.Stage, result)
	}
	result.TaskStatus = snapshot.Status
	return result
}

func classifyReviewResult(result StageResult) reviewDecision {
	switch normalizeNextAction(result.NextAction) {
	case "await_human":
		return reviewDecisionAwaitHuman
	case "return_to_developer":
		return reviewDecisionRequestChange
	case "open_task_pr":
		return reviewDecisionPass
	}

	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "blocked":
		return reviewDecisionAwaitHuman
	case "needs_changes":
		return reviewDecisionRequestChange
	case "pass":
		return reviewDecisionPass
	default:
		return reviewDecisionRequestChange
	}
}

func stageFailureFingerprint(stage Stage, result StageResult) string {
	if strings.TrimSpace(result.FailureFingerprint) != "" {
		return strings.TrimSpace(result.FailureFingerprint)
	}
	return fmt.Sprintf("%s:%s:%s", stage, strings.TrimSpace(result.Status), strings.TrimSpace(result.NextAction))
}

func blockedHumanReason(result StageResult) string {
	summary := strings.TrimSpace(result.Summary)
	nextAction := strings.TrimSpace(result.NextAction)
	if summary == "" {
		return nextAction
	}
	if nextAction == "" {
		return summary
	}
	return fmt.Sprintf("%s (suggested action: %s)", summary, nextAction)
}

func transitionBudgetFailureFingerprint(snapshot taskRuntimeSnapshot, stageResults []StageResult) string {
	if len(stageResults) > 0 {
		last := stageResults[len(stageResults)-1]
		if failureFingerprint := strings.TrimSpace(last.FailureFingerprint); failureFingerprint != "" {
			return failureFingerprint
		}
	}
	if failureFingerprint := optionalString(snapshot.CurrentFailureFingerprint); failureFingerprint != "" {
		return failureFingerprint
	}
	status := strings.TrimSpace(snapshot.Status)
	if status == "" {
		status = "unknown"
	}
	return "orchestrator:max_stage_transitions:" + status
}

func transitionBudgetStageResult(
	snapshot taskRuntimeSnapshot,
	stageResults []StageResult,
	resumeRefs stageArtifactRefsSnapshot,
	failureFingerprint string,
) (StageResult, bool) {
	if len(stageResults) > 0 {
		last := stageResults[len(stageResults)-1]
		if strings.TrimSpace(last.FailureFingerprint) == "" {
			last.FailureFingerprint = strings.TrimSpace(failureFingerprint)
		}
		return last, true
	}

	stage := resumeStageForTaskStatus(snapshot.Status, resumeRefs)
	artifactRefs := artifactRefsForTaskStatus(snapshot.Status, resumeRefs)
	if stage == "" && !hasAnyArtifactRefs(artifactRefs) && strings.TrimSpace(failureFingerprint) == "" {
		return StageResult{}, false
	}

	stageResult := StageResult{
		Stage:              stage,
		Attempt:            snapshot.AttemptTotal,
		TaskStatus:         snapshot.Status,
		FailureFingerprint: strings.TrimSpace(failureFingerprint),
		ArtifactRefs:       artifactRefs,
	}
	if stage != "" {
		stageResult.AgentType = agentTypeForStage(stage)
	}
	return stageResult, true
}

func reviewFailureFingerprint(attempt int, findings []agentrun.Finding, fallback string) string {
	if len(findings) > 0 && strings.TrimSpace(findings[0].FindingFingerprint) != "" {
		return strings.TrimSpace(findings[0].FindingFingerprint)
	}
	return fmt.Sprintf("review:%02d:%s", attempt, strings.TrimSpace(fallback))
}

func normalizedReviewerLens(rawLens string) (string, error) {
	rawLens = strings.TrimSpace(rawLens)
	if rawLens == "" {
		return defaultReviewerLens, nil
	}

	lens, ok := agentrun.NormalizeReviewerLens(rawLens)
	if !ok {
		return "", fmt.Errorf("invalid reviewer lens %q (allowed: %s)", rawLens, strings.Join(agentrun.AllowedReviewerLenses(), ", "))
	}
	return lens, nil
}

func validateReviewObservationContract(result StageResult, findings []agentrun.Finding, lens string) error {
	if len(findings) > 0 || reviewObservationAllowsEmptyFindings(result.Status) {
		return nil
	}

	normalizedLens, ok := agentrun.NormalizeReviewerLens(lens)
	if !ok {
		normalizedLens = strings.TrimSpace(lens)
	}
	if normalizedLens == "" {
		normalizedLens = defaultReviewerLens
	}
	return fmt.Errorf("reviewer lens %s returned %s without structured findings", normalizedLens, strings.ToLower(strings.TrimSpace(result.Status)))
}

func reviewObservationAllowsEmptyFindings(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "completed", "pass", "passed", "passed_with_warning":
		return true
	default:
		return false
	}
}

func normalizeFindings(findings []agentrun.Finding, fallbackLens string) ([]agentrun.Finding, error) {
	normalized := make([]agentrun.Finding, 0, len(findings))
	for _, finding := range findings {
		normalizedFinding, err := normalizeReviewFinding(finding, fallbackLens)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, normalizedFinding)
	}
	return normalized, nil
}

func normalizeReviewFinding(finding agentrun.Finding, fallbackLens string) (agentrun.Finding, error) {
	finding.ReviewerID = strings.TrimSpace(finding.ReviewerID)
	if finding.ReviewerID == "" {
		return agentrun.Finding{}, errors.New("missing reviewer_id")
	}

	lens := strings.TrimSpace(finding.Lens)
	if lens == "" {
		lens = strings.TrimSpace(fallbackLens)
	}
	normalizedLens, ok := agentrun.NormalizeReviewerLens(lens)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid lens %q", lens)
	}

	finding.Lens = normalizedLens
	finding.Severity, ok = agentrun.NormalizeFindingSeverity(finding.Severity)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid severity %q", strings.TrimSpace(finding.Severity))
	}
	finding.Confidence, ok = agentrun.NormalizeFindingConfidence(finding.Confidence)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid confidence %q", strings.TrimSpace(finding.Confidence))
	}
	finding.Category = strings.TrimSpace(finding.Category)
	finding.Summary = strings.TrimSpace(finding.Summary)
	finding.Evidence = strings.TrimSpace(finding.Evidence)
	finding.FindingFingerprint = strings.TrimSpace(finding.FindingFingerprint)
	finding.SuggestedAction = strings.TrimSpace(finding.SuggestedAction)
	finding.FileRefs = normalizeStringSlice(finding.FileRefs)

	switch {
	case finding.Category == "":
		return agentrun.Finding{}, errors.New("missing category")
	case finding.Summary == "":
		return agentrun.Finding{}, errors.New("missing summary")
	case finding.Evidence == "":
		return agentrun.Finding{}, errors.New("missing evidence")
	case finding.FindingFingerprint == "":
		return agentrun.Finding{}, errors.New("missing finding_fingerprint")
	case finding.SuggestedAction == "":
		return agentrun.Finding{}, errors.New("missing suggested_action")
	default:
		return finding, nil
	}
}

func reviewFindingAggregateKey(finding agentrun.Finding) string {
	key := strings.TrimSpace(finding.FindingFingerprint)
	if key != "" {
		return key
	}
	return fmt.Sprintf("%s|%s|%s", strings.TrimSpace(finding.ReviewerID), strings.TrimSpace(finding.Lens), strings.TrimSpace(finding.Summary))
}

func strongerReviewConfidence(current, candidate string, reviewerCount int) string {
	if reviewerCount > 1 {
		return "high"
	}
	current = strings.ToLower(strings.TrimSpace(current))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if reviewConfidenceRank(candidate) > reviewConfidenceRank(current) {
		return candidate
	}
	if current != "" {
		return current
	}
	return candidate
}

func strongerReviewSeverity(current, candidate string) string {
	current = normalizedReviewSeverity(current)
	candidate = normalizedReviewSeverity(candidate)
	if reviewSeverityRank(candidate) > reviewSeverityRank(current) {
		return candidate
	}
	if current != "" {
		return current
	}
	return candidate
}

func normalizedReviewSeverity(severity string) string {
	normalized, _ := agentrun.NormalizeFindingSeverity(severity)
	return normalized
}

func reviewConfidenceRank(confidence string) int {
	switch normalized, _ := agentrun.NormalizeFindingConfidence(confidence); normalized {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func reviewSeverityRank(severity string) int {
	switch normalizedReviewSeverity(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func normalizeStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func dedupeOrderedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range normalizeStringSlice(values) {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func hasAnyArtifactRefs(refs agentrun.ArtifactRefs) bool {
	return strings.TrimSpace(refs.Log) != "" ||
		strings.TrimSpace(refs.Worktree) != "" ||
		strings.TrimSpace(refs.Patch) != "" ||
		strings.TrimSpace(refs.Report) != ""
}

func shouldPersistEscalationStageResult(stageResult StageResult) bool {
	return stageResult.Stage != "" ||
		strings.TrimSpace(stageResult.FailureFingerprint) != "" ||
		hasAnyArtifactRefs(stageResult.ArtifactRefs)
}

func stageForTaskStatus(status string) Stage {
	switch strings.TrimSpace(status) {
	case "dev_in_progress", "qa_failed", "review_failed":
		return StageDeveloper
	case "qa_in_progress":
		return StageQA
	case "review_in_progress", "pr_open":
		return StageReview
	default:
		return ""
	}
}

func resumeStageForTaskStatus(status string, refs stageArtifactRefsSnapshot) Stage {
	if stage := stageForTaskStatus(status); stage != "" {
		return stage
	}
	return refs.LastStage
}

func nextActionForTaskStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pr_open":
		return "open_task_pr"
	case "awaiting_human":
		return "await_human"
	case "blocked", "escalated", "done", "canceled":
		return "stop"
	default:
		return "continue"
	}
}

func summaryForTaskStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "pr_open":
		return "task is ready for Task PR handoff"
	case "awaiting_human":
		return "task is waiting for human intervention"
	case "blocked":
		return "task is blocked"
	case "escalated":
		return "task is escalated"
	case "done":
		return "task is done"
	case "canceled":
		return "task is canceled"
	default:
		return "task stage machine resumed without additional work"
	}
}

func artifactRefsForTaskStatus(status string, refs stageArtifactRefsSnapshot) agentrun.ArtifactRefs {
	switch resumeStageForTaskStatus(status, refs) {
	case StageReview:
		return firstArtifactRefs(refs.refsFor(StageReview), refs.LastStageArtifactRefs, refs.QA, refs.Developer)
	case StageQA:
		return firstArtifactRefs(refs.refsFor(StageQA), refs.LastStageArtifactRefs, refs.Developer, refs.Review)
	case StageDeveloper:
		return firstArtifactRefs(refs.refsFor(StageDeveloper), refs.LastStageArtifactRefs, refs.QA, refs.Review)
	default:
		return firstArtifactRefs(refs.LastStageArtifactRefs, refs.Review, refs.QA, refs.Developer)
	}
}

func firstArtifactRefs(candidates ...agentrun.ArtifactRefs) agentrun.ArtifactRefs {
	for _, candidate := range candidates {
		if hasAnyArtifactRefs(candidate) {
			return candidate
		}
	}
	return agentrun.ArtifactRefs{}
}

func taskEventID(taskID, eventType string, attempt int) string {
	return fmt.Sprintf("evt_%s_%s_%02d", normalizeEventEntityID(taskID), strings.TrimSpace(eventType), attempt)
}

func taskEventIDKey(taskID, eventType string, attempt int) string {
	return fmt.Sprintf("%s:%s:%s:%02d", orchestratorSource, strings.TrimSpace(taskID), strings.TrimSpace(eventType), attempt)
}

func reviewFindingID(taskID, reviewEventID, reviewerID, fingerprint string) string {
	parts := []string{taskID, reviewEventID, reviewerID, fingerprint}
	return "finding_" + normalizeEventEntityID(strings.Join(parts, "_"))
}

func normalizeEventEntityID(value string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func currentUTCTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func emptyStringPointer() *string {
	value := ""
	return &value
}

func intPointer(value int) *int {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func copyFindings(findings []agentrun.Finding) []agentrun.Finding {
	if len(findings) == 0 {
		return nil
	}
	copied := make([]agentrun.Finding, 0, len(findings))
	for _, finding := range findings {
		copyFinding := finding
		copyFinding.FileRefs = append([]string(nil), finding.FileRefs...)
		copied = append(copied, copyFinding)
	}
	return copied
}

func copyStageResults(values []StageResult) []StageResult {
	if len(values) == 0 {
		return nil
	}
	copied := make([]StageResult, 0, len(values))
	for _, value := range values {
		copyValue := value
		copyValue.Findings = copyFindings(value.Findings)
		copied = append(copied, copyValue)
	}
	return copied
}

func copyTaskProjection(value store.TaskProjection) *store.TaskProjection {
	copied := value
	return &copied
}

func copySprintProjection(value *store.SprintProjection) *store.SprintProjection {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
