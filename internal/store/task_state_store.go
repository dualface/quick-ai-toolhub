package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

const (
	TaskStateStoreOpAppendEvent             TaskStateStoreOperation = "append_event"
	TaskStateStoreOpLoadTaskProjection      TaskStateStoreOperation = "load_task_projection"
	TaskStateStoreOpLoadSprintProjection    TaskStateStoreOperation = "load_sprint_projection"
	TaskStateStoreOpUpdateTaskState         TaskStateStoreOperation = "update_task_state"
	TaskStateStoreOpUpdateSprintState       TaskStateStoreOperation = "update_sprint_state"
	TaskStateStoreOpEnqueueOutboxAction     TaskStateStoreOperation = "enqueue_outbox_action"
	TaskStateStoreOpLoadPendingOutboxAction TaskStateStoreOperation = "load_pending_outbox_actions"

	taskStateStoreErrorInvalidRequest = "invalid_request"
	taskStateStoreErrorNotFound       = "not_found"
	taskStateStoreErrorStoreNotOpen   = "store_not_open"
	taskStateStoreErrorInternal       = "internal_failure"

	outboxStatusPending = "pending"
)

type TaskStateStoreOperation string

type TaskStateStoreRequest struct {
	Op      TaskStateStoreOperation `json:"op"`
	Payload json.RawMessage         `json:"payload"`
}

type TaskStateStoreResponse struct {
	OK    bool                        `json:"ok"`
	Data  *TaskStateStoreResponseData `json:"data,omitempty"`
	Error *ToolError                  `json:"error,omitempty"`
}

type TaskStateStoreResponseData struct {
	AppendEvent              *AppendEventResult              `json:"append_event,omitempty"`
	LoadTaskProjection       *TaskProjection                 `json:"load_task_projection,omitempty"`
	LoadSprintProjection     *SprintProjection               `json:"load_sprint_projection,omitempty"`
	UpdateTaskState          *UpdateTaskStateResult          `json:"update_task_state,omitempty"`
	UpdateSprintState        *UpdateSprintStateResult        `json:"update_sprint_state,omitempty"`
	EnqueueOutboxAction      *EnqueueOutboxActionResult      `json:"enqueue_outbox_action,omitempty"`
	LoadPendingOutboxActions *LoadPendingOutboxActionsResult `json:"load_pending_outbox_actions,omitempty"`
}

type ToolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type AppendEventPayload struct {
	EventID        string         `json:"event_id"`
	EntityType     string         `json:"entity_type"`
	EntityID       string         `json:"entity_id"`
	SprintID       *string        `json:"sprint_id,omitempty"`
	TaskID         *string        `json:"task_id,omitempty"`
	EventType      string         `json:"event_type"`
	Source         string         `json:"source"`
	Attempt        *int           `json:"attempt,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	PayloadJSON    map[string]any `json:"payload_json"`
	OccurredAt     string         `json:"occurred_at"`
}

type LoadTaskProjectionPayload struct {
	TaskID string `json:"task_id"`
}

type LoadSprintProjectionPayload struct {
	SprintID string `json:"sprint_id"`
}

type UpdateTaskStatePayload struct {
	TaskID                    string  `json:"task_id"`
	Status                    string  `json:"status"`
	AttemptTotal              *int    `json:"attempt_total,omitempty"`
	QAFailCount               *int    `json:"qa_fail_count,omitempty"`
	ReviewFailCount           *int    `json:"review_fail_count,omitempty"`
	CIFailCount               *int    `json:"ci_fail_count,omitempty"`
	CurrentFailureFingerprint *string `json:"current_failure_fingerprint,omitempty"`
	ActivePRNumber            *int    `json:"active_pr_number,omitempty"`
	TaskBranch                *string `json:"task_branch,omitempty"`
	WorktreePath              *string `json:"worktree_path,omitempty"`
	NeedsHuman                *bool   `json:"needs_human,omitempty"`
	HumanReason               *string `json:"human_reason,omitempty"`
}

type UpdateSprintStatePayload struct {
	SprintID             string  `json:"sprint_id"`
	Status               string  `json:"status"`
	ActiveSprintPRNumber *int    `json:"active_sprint_pr_number,omitempty"`
	SprintBranch         *string `json:"sprint_branch,omitempty"`
	NeedsHuman           *bool   `json:"needs_human,omitempty"`
	HumanReason          *string `json:"human_reason,omitempty"`
}

type EnqueueOutboxActionPayload struct {
	ActionID           string         `json:"action_id"`
	EntityType         string         `json:"entity_type"`
	EntityID           string         `json:"entity_id"`
	ActionType         string         `json:"action_type"`
	GitHubTargetType   string         `json:"github_target_type"`
	GitHubTargetNumber *int           `json:"github_target_number,omitempty"`
	IdempotencyKey     string         `json:"idempotency_key"`
	RequestPayloadJSON map[string]any `json:"request_payload_json"`
	NextAttemptAt      *string        `json:"next_attempt_at,omitempty"`
}

type LoadPendingOutboxActionsPayload struct {
	Limit int `json:"limit"`
}

type AppendEventResult struct {
	EventID      string `json:"event_id"`
	Deduplicated bool   `json:"deduplicated"`
}

type UpdateTaskStateResult struct {
	Task TaskProjection `json:"task"`
}

type UpdateSprintStateResult struct {
	Sprint SprintProjection `json:"sprint"`
}

type EnqueueOutboxActionResult struct {
	ActionID     string `json:"action_id"`
	Deduplicated bool   `json:"deduplicated"`
}

type LoadPendingOutboxActionsResult struct {
	Actions []OutboxAction `json:"actions"`
}

type SprintProjection struct {
	SprintID             string  `json:"sprint_id"`
	SequenceNo           int     `json:"sequence_no"`
	GitHubIssueNumber    int     `json:"github_issue_number"`
	Status               string  `json:"status"`
	SprintBranch         *string `json:"sprint_branch,omitempty"`
	ActiveSprintPRNumber *int    `json:"active_sprint_pr_number,omitempty"`
	NeedsHuman           bool    `json:"needs_human"`
	HumanReason          *string `json:"human_reason,omitempty"`
}

type TaskProjection struct {
	TaskID            string  `json:"task_id"`
	SprintID          string  `json:"sprint_id"`
	TaskLocalID       string  `json:"task_local_id"`
	SequenceNo        int     `json:"sequence_no"`
	GitHubIssueNumber int     `json:"github_issue_number"`
	Status            string  `json:"status"`
	ActivePRNumber    *int    `json:"active_pr_number,omitempty"`
	TaskBranch        *string `json:"task_branch,omitempty"`
	WorktreePath      *string `json:"worktree_path,omitempty"`
	NeedsHuman        bool    `json:"needs_human"`
	HumanReason       *string `json:"human_reason,omitempty"`
}

type TaskFailureSignals struct {
	TaskID                    string  `json:"task_id"`
	AttemptTotal              int     `json:"attempt_total"`
	QAFailCount               int     `json:"qa_fail_count"`
	ReviewFailCount           int     `json:"review_fail_count"`
	CIFailCount               int     `json:"ci_fail_count"`
	CurrentFailureFingerprint *string `json:"current_failure_fingerprint,omitempty"`
}

type OutboxAction struct {
	ActionID           string         `json:"action_id"`
	EntityType         string         `json:"entity_type"`
	EntityID           string         `json:"entity_id"`
	ActionType         string         `json:"action_type"`
	GitHubTargetType   string         `json:"github_target_type"`
	GitHubTargetNumber *int           `json:"github_target_number,omitempty"`
	IdempotencyKey     string         `json:"idempotency_key"`
	RequestPayloadJSON map[string]any `json:"request_payload_json"`
	Status             string         `json:"status"`
	AttemptCount       int            `json:"attempt_count"`
	LastError          *string        `json:"last_error,omitempty"`
	NextAttemptAt      *string        `json:"next_attempt_at,omitempty"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	CompletedAt        *string        `json:"completed_at,omitempty"`
}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

type notFoundError struct {
	resource string
	id       string
}

func (e *notFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.resource, e.id)
}

type eventRecord struct {
	bun.BaseModel `bun:"table:events"`

	EventID        string  `bun:"event_id,pk"`
	EntityType     string  `bun:"entity_type"`
	EntityID       string  `bun:"entity_id"`
	SprintID       *string `bun:"sprint_id"`
	TaskID         *string `bun:"task_id"`
	EventType      string  `bun:"event_type"`
	Source         string  `bun:"source"`
	Attempt        *int    `bun:"attempt"`
	IdempotencyKey string  `bun:"idempotency_key"`
	PayloadJSON    string  `bun:"payload_json"`
	OccurredAt     string  `bun:"occurred_at"`
	RecordedAt     string  `bun:"recorded_at"`
}

type taskProjectionRecord struct {
	bun.BaseModel `bun:"table:tasks"`

	TaskID                    string  `bun:"task_id,pk"`
	SprintID                  string  `bun:"sprint_id"`
	TaskLocalID               string  `bun:"task_local_id"`
	SequenceNo                int     `bun:"sequence_no"`
	GitHubIssueNumber         int     `bun:"github_issue_number"`
	Status                    string  `bun:"status"`
	AttemptTotal              int     `bun:"attempt_total"`
	QAFailCount               int     `bun:"qa_fail_count"`
	ReviewFailCount           int     `bun:"review_fail_count"`
	CIFailCount               int     `bun:"ci_fail_count"`
	CurrentFailureFingerprint *string `bun:"current_failure_fingerprint"`
	ActivePRNumber            *int    `bun:"active_pr_number"`
	TaskBranch                *string `bun:"task_branch"`
	WorktreePath              *string `bun:"worktree_path"`
	NeedsHuman                bool    `bun:"needs_human"`
	HumanReason               *string `bun:"human_reason"`
}

type sprintProjectionRecord struct {
	bun.BaseModel `bun:"table:sprints"`

	SprintID             string  `bun:"sprint_id,pk"`
	SequenceNo           int     `bun:"sequence_no"`
	GitHubIssueNumber    int     `bun:"github_issue_number"`
	Status               string  `bun:"status"`
	SprintBranch         *string `bun:"sprint_branch"`
	ActiveSprintPRNumber *int    `bun:"active_sprint_pr_number"`
	NeedsHuman           bool    `bun:"needs_human"`
	HumanReason          *string `bun:"human_reason"`
}

type outboxActionRecord struct {
	bun.BaseModel `bun:"table:outbox_actions"`

	ActionID           string  `bun:"action_id,pk"`
	EntityType         string  `bun:"entity_type"`
	EntityID           string  `bun:"entity_id"`
	ActionType         string  `bun:"action_type"`
	GitHubTargetType   string  `bun:"github_target_type"`
	GitHubTargetNumber *int    `bun:"github_target_number"`
	IdempotencyKey     string  `bun:"idempotency_key"`
	RequestPayloadJSON string  `bun:"request_payload_json"`
	Status             string  `bun:"status"`
	AttemptCount       int     `bun:"attempt_count"`
	LastError          *string `bun:"last_error"`
	NextAttemptAt      *string `bun:"next_attempt_at"`
	CreatedAt          string  `bun:"created_at"`
	UpdatedAt          string  `bun:"updated_at"`
	CompletedAt        *string `bun:"completed_at"`
}

func (s *Service) ExecuteTaskStateStoreTool(ctx context.Context, req TaskStateStoreRequest) TaskStateStoreResponse {
	base, err := s.BaseStore()
	if err != nil {
		return taskStateStoreFailure(err)
	}
	return base.ExecuteTaskStateStoreTool(ctx, req)
}

func (s BaseStore) ExecuteTaskStateStoreTool(ctx context.Context, req TaskStateStoreRequest) TaskStateStoreResponse {
	switch req.Op {
	case TaskStateStoreOpAppendEvent:
		var payload AppendEventPayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		result, err := s.AppendEvent(ctx, payload)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				AppendEvent: &result,
			},
		}

	case TaskStateStoreOpLoadTaskProjection:
		var payload LoadTaskProjectionPayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		projection, err := s.LoadTaskProjection(ctx, payload.TaskID)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				LoadTaskProjection: &projection,
			},
		}

	case TaskStateStoreOpLoadSprintProjection:
		var payload LoadSprintProjectionPayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		projection, err := s.LoadSprintProjection(ctx, payload.SprintID)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				LoadSprintProjection: &projection,
			},
		}

	case TaskStateStoreOpUpdateTaskState:
		var payload UpdateTaskStatePayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		result, err := s.UpdateTaskState(ctx, payload)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				UpdateTaskState: &UpdateTaskStateResult{
					Task: result,
				},
			},
		}

	case TaskStateStoreOpUpdateSprintState:
		var payload UpdateSprintStatePayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		result, err := s.UpdateSprintState(ctx, payload)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				UpdateSprintState: &UpdateSprintStateResult{
					Sprint: result,
				},
			},
		}

	case TaskStateStoreOpEnqueueOutboxAction:
		var payload EnqueueOutboxActionPayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		result, err := s.EnqueueOutboxAction(ctx, payload)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				EnqueueOutboxAction: &result,
			},
		}

	case TaskStateStoreOpLoadPendingOutboxAction:
		var payload LoadPendingOutboxActionsPayload
		if err := decodeTaskStateStorePayload(req.Payload, &payload); err != nil {
			return taskStateStoreFailure(err)
		}

		actions, err := s.LoadPendingOutboxActions(ctx, payload.Limit)
		if err != nil {
			return taskStateStoreFailure(err)
		}

		return TaskStateStoreResponse{
			OK: true,
			Data: &TaskStateStoreResponseData{
				LoadPendingOutboxActions: &LoadPendingOutboxActionsResult{
					Actions: actions,
				},
			},
		}
	}

	return taskStateStoreFailure(newValidationError("unsupported op %q", req.Op))
}

func (s BaseStore) AppendEvent(ctx context.Context, payload AppendEventPayload) (AppendEventResult, error) {
	if err := validateAppendEventPayload(payload); err != nil {
		return AppendEventResult{}, err
	}

	db, err := s.requireDB()
	if err != nil {
		return AppendEventResult{}, err
	}

	payloadJSON, err := marshalJSONObject(payload.PayloadJSON)
	if err != nil {
		return AppendEventResult{}, err
	}

	record := eventRecord{
		EventID:        payload.EventID,
		EntityType:     payload.EntityType,
		EntityID:       payload.EntityID,
		SprintID:       normalizeOptionalString(payload.SprintID),
		TaskID:         normalizeOptionalString(payload.TaskID),
		EventType:      payload.EventType,
		Source:         payload.Source,
		Attempt:        payload.Attempt,
		IdempotencyKey: payload.IdempotencyKey,
		PayloadJSON:    payloadJSON,
		OccurredAt:     payload.OccurredAt,
		RecordedAt:     currentUTCTimestamp(),
	}

	result, err := db.NewInsert().
		Model(&record).
		On("CONFLICT (idempotency_key) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return AppendEventResult{}, fmt.Errorf("insert event: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return AppendEventResult{}, fmt.Errorf("read inserted event result: %w", err)
	}
	if rowsAffected == 0 {
		eventID, err := lookupEventIDByIdempotencyKey(ctx, db, payload.IdempotencyKey)
		if err != nil {
			return AppendEventResult{}, err
		}
		return AppendEventResult{
			EventID:      eventID,
			Deduplicated: true,
		}, nil
	}

	return AppendEventResult{
		EventID:      payload.EventID,
		Deduplicated: false,
	}, nil
}

func (s BaseStore) LoadTaskProjection(ctx context.Context, taskID string) (TaskProjection, error) {
	if strings.TrimSpace(taskID) == "" {
		return TaskProjection{}, newValidationError("task_id is required")
	}

	db, err := s.requireDB()
	if err != nil {
		return TaskProjection{}, err
	}

	return loadTaskProjection(ctx, db, taskID)
}

func (s BaseStore) LoadTaskFailureSignals(ctx context.Context, taskID string) (TaskFailureSignals, error) {
	if strings.TrimSpace(taskID) == "" {
		return TaskFailureSignals{}, newValidationError("task_id is required")
	}

	db, err := s.requireDB()
	if err != nil {
		return TaskFailureSignals{}, err
	}

	return loadTaskFailureSignals(ctx, db, taskID)
}

func (s BaseStore) LoadSprintProjection(ctx context.Context, sprintID string) (SprintProjection, error) {
	if strings.TrimSpace(sprintID) == "" {
		return SprintProjection{}, newValidationError("sprint_id is required")
	}

	db, err := s.requireDB()
	if err != nil {
		return SprintProjection{}, err
	}

	return loadSprintProjection(ctx, db, sprintID)
}

func (s BaseStore) UpdateTaskState(ctx context.Context, payload UpdateTaskStatePayload) (TaskProjection, error) {
	if err := validateUpdateTaskStatePayload(payload); err != nil {
		return TaskProjection{}, err
	}

	db, err := s.requireDB()
	if err != nil {
		return TaskProjection{}, err
	}

	update := db.NewUpdate().
		Model((*taskProjectionRecord)(nil)).
		Set("status = ?", payload.Status).
		Set("updated_at = ?", currentUTCTimestamp()).
		Where("task_id = ?", payload.TaskID)

	if payload.AttemptTotal != nil {
		update.Set("attempt_total = ?", *payload.AttemptTotal)
	}
	if payload.QAFailCount != nil {
		update.Set("qa_fail_count = ?", *payload.QAFailCount)
	}
	if payload.ReviewFailCount != nil {
		update.Set("review_fail_count = ?", *payload.ReviewFailCount)
	}
	if payload.CIFailCount != nil {
		update.Set("ci_fail_count = ?", *payload.CIFailCount)
	}
	if payload.CurrentFailureFingerprint != nil {
		update.Set("current_failure_fingerprint = ?", normalizeOptionalString(payload.CurrentFailureFingerprint))
	}
	if payload.ActivePRNumber != nil {
		update.Set("active_pr_number = ?", *payload.ActivePRNumber)
	}
	if payload.TaskBranch != nil {
		update.Set("task_branch = ?", normalizeOptionalString(payload.TaskBranch))
	}
	if payload.WorktreePath != nil {
		update.Set("worktree_path = ?", normalizeOptionalString(payload.WorktreePath))
	}
	if payload.NeedsHuman != nil {
		update.Set("needs_human = ?", *payload.NeedsHuman)
	}
	if payload.HumanReason != nil {
		update.Set("human_reason = ?", normalizeOptionalString(payload.HumanReason))
	}

	result, err := update.Exec(ctx)
	if err != nil {
		return TaskProjection{}, fmt.Errorf("update task state: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return TaskProjection{}, fmt.Errorf("read updated task result: %w", err)
	}
	if rowsAffected == 0 {
		return TaskProjection{}, &notFoundError{resource: "task", id: payload.TaskID}
	}

	return loadTaskProjection(ctx, db, payload.TaskID)
}

func (s BaseStore) UpdateSprintState(ctx context.Context, payload UpdateSprintStatePayload) (SprintProjection, error) {
	if err := validateUpdateSprintStatePayload(payload); err != nil {
		return SprintProjection{}, err
	}

	db, err := s.requireDB()
	if err != nil {
		return SprintProjection{}, err
	}

	update := db.NewUpdate().
		Model((*sprintProjectionRecord)(nil)).
		Set("status = ?", payload.Status).
		Set("updated_at = ?", currentUTCTimestamp()).
		Where("sprint_id = ?", payload.SprintID)

	if payload.ActiveSprintPRNumber != nil {
		update.Set("active_sprint_pr_number = ?", *payload.ActiveSprintPRNumber)
	}
	if payload.SprintBranch != nil {
		update.Set("sprint_branch = ?", *payload.SprintBranch)
	}
	if payload.NeedsHuman != nil {
		update.Set("needs_human = ?", *payload.NeedsHuman)
	}
	if payload.HumanReason != nil {
		update.Set("human_reason = ?", *payload.HumanReason)
	}

	result, err := update.Exec(ctx)
	if err != nil {
		return SprintProjection{}, fmt.Errorf("update sprint state: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return SprintProjection{}, fmt.Errorf("read updated sprint result: %w", err)
	}
	if rowsAffected == 0 {
		return SprintProjection{}, &notFoundError{resource: "sprint", id: payload.SprintID}
	}

	return loadSprintProjection(ctx, db, payload.SprintID)
}

func (s BaseStore) EnqueueOutboxAction(ctx context.Context, payload EnqueueOutboxActionPayload) (EnqueueOutboxActionResult, error) {
	if err := validateEnqueueOutboxActionPayload(payload); err != nil {
		return EnqueueOutboxActionResult{}, err
	}

	db, err := s.requireDB()
	if err != nil {
		return EnqueueOutboxActionResult{}, err
	}

	requestPayloadJSON, err := marshalJSONObject(payload.RequestPayloadJSON)
	if err != nil {
		return EnqueueOutboxActionResult{}, err
	}

	now := currentUTCTimestamp()
	record := outboxActionRecord{
		ActionID:           payload.ActionID,
		EntityType:         payload.EntityType,
		EntityID:           payload.EntityID,
		ActionType:         payload.ActionType,
		GitHubTargetType:   payload.GitHubTargetType,
		GitHubTargetNumber: payload.GitHubTargetNumber,
		IdempotencyKey:     payload.IdempotencyKey,
		RequestPayloadJSON: requestPayloadJSON,
		Status:             outboxStatusPending,
		AttemptCount:       0,
		NextAttemptAt:      normalizeOptionalString(payload.NextAttemptAt),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	result, err := db.NewInsert().
		Model(&record).
		On("CONFLICT (idempotency_key) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return EnqueueOutboxActionResult{}, fmt.Errorf("insert outbox action: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return EnqueueOutboxActionResult{}, fmt.Errorf("read inserted outbox action result: %w", err)
	}
	if rowsAffected == 0 {
		actionID, err := lookupOutboxActionIDByIdempotencyKey(ctx, db, payload.IdempotencyKey)
		if err != nil {
			return EnqueueOutboxActionResult{}, err
		}
		return EnqueueOutboxActionResult{
			ActionID:     actionID,
			Deduplicated: true,
		}, nil
	}

	return EnqueueOutboxActionResult{
		ActionID:     payload.ActionID,
		Deduplicated: false,
	}, nil
}

func (s BaseStore) LoadPendingOutboxActions(ctx context.Context, limit int) ([]OutboxAction, error) {
	if limit <= 0 {
		return nil, newValidationError("limit must be greater than zero")
	}

	db, err := s.requireDB()
	if err != nil {
		return nil, err
	}

	var records []outboxActionRecord
	now := currentUTCTimestamp()
	if err := db.NewSelect().
		Model(&records).
		Where("status = ?", outboxStatusPending).
		Where("(next_attempt_at IS NULL OR julianday(next_attempt_at) <= julianday(?))", now).
		OrderExpr("CASE WHEN next_attempt_at IS NULL THEN 0 ELSE 1 END").
		OrderExpr("next_attempt_at ASC").
		OrderExpr("created_at ASC").
		OrderExpr("action_id ASC").
		Limit(limit).
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("load pending outbox actions: %w", err)
	}

	actions := make([]OutboxAction, 0, len(records))
	for _, record := range records {
		action, err := record.toOutboxAction()
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func loadTaskProjection(ctx context.Context, db bun.IDB, taskID string) (TaskProjection, error) {
	var record taskProjectionRecord
	err := db.NewSelect().
		Model(&record).
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
		Where("task_id = ?", taskID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskProjection{}, &notFoundError{resource: "task", id: taskID}
		}
		return TaskProjection{}, fmt.Errorf("load task projection: %w", err)
	}
	return record.toProjection(), nil
}

func loadTaskFailureSignals(ctx context.Context, db bun.IDB, taskID string) (TaskFailureSignals, error) {
	var signals TaskFailureSignals
	err := db.NewSelect().
		TableExpr("tasks").
		Column(
			"task_id",
			"attempt_total",
			"qa_fail_count",
			"review_fail_count",
			"ci_fail_count",
			"current_failure_fingerprint",
		).
		Where("task_id = ?", taskID).
		Limit(1).
		Scan(ctx, &signals)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskFailureSignals{}, &notFoundError{resource: "task", id: taskID}
		}
		return TaskFailureSignals{}, fmt.Errorf("load task failure signals: %w", err)
	}
	return signals, nil
}

func loadSprintProjection(ctx context.Context, db bun.IDB, sprintID string) (SprintProjection, error) {
	var record sprintProjectionRecord
	err := db.NewSelect().
		Model(&record).
		Column(
			"sprint_id",
			"sequence_no",
			"github_issue_number",
			"status",
			"sprint_branch",
			"active_sprint_pr_number",
			"needs_human",
			"human_reason",
		).
		Where("sprint_id = ?", sprintID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SprintProjection{}, &notFoundError{resource: "sprint", id: sprintID}
		}
		return SprintProjection{}, fmt.Errorf("load sprint projection: %w", err)
	}
	return record.toProjection(), nil
}

func lookupEventIDByIdempotencyKey(ctx context.Context, db bun.IDB, idempotencyKey string) (string, error) {
	var eventID string
	err := db.NewSelect().
		TableExpr("events").
		Column("event_id").
		Where("idempotency_key = ?", idempotencyKey).
		Limit(1).
		Scan(ctx, &eventID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("load existing event for idempotency key %q: %w", idempotencyKey, err)
		}
		return "", fmt.Errorf("load existing event for idempotency key %q: %w", idempotencyKey, err)
	}
	return eventID, nil
}

func lookupOutboxActionIDByIdempotencyKey(ctx context.Context, db bun.IDB, idempotencyKey string) (string, error) {
	var actionID string
	err := db.NewSelect().
		TableExpr("outbox_actions").
		Column("action_id").
		Where("idempotency_key = ?", idempotencyKey).
		Limit(1).
		Scan(ctx, &actionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("load existing outbox action for idempotency key %q: %w", idempotencyKey, err)
		}
		return "", fmt.Errorf("load existing outbox action for idempotency key %q: %w", idempotencyKey, err)
	}
	return actionID, nil
}

func (r taskProjectionRecord) toProjection() TaskProjection {
	return TaskProjection{
		TaskID:            r.TaskID,
		SprintID:          r.SprintID,
		TaskLocalID:       r.TaskLocalID,
		SequenceNo:        r.SequenceNo,
		GitHubIssueNumber: r.GitHubIssueNumber,
		Status:            r.Status,
		ActivePRNumber:    r.ActivePRNumber,
		TaskBranch:        r.TaskBranch,
		WorktreePath:      r.WorktreePath,
		NeedsHuman:        r.NeedsHuman,
		HumanReason:       r.HumanReason,
	}
}

func (r sprintProjectionRecord) toProjection() SprintProjection {
	return SprintProjection{
		SprintID:             r.SprintID,
		SequenceNo:           r.SequenceNo,
		GitHubIssueNumber:    r.GitHubIssueNumber,
		Status:               r.Status,
		SprintBranch:         r.SprintBranch,
		ActiveSprintPRNumber: r.ActiveSprintPRNumber,
		NeedsHuman:           r.NeedsHuman,
		HumanReason:          r.HumanReason,
	}
}

func (r outboxActionRecord) toOutboxAction() (OutboxAction, error) {
	requestPayload, err := unmarshalJSONObject(r.RequestPayloadJSON)
	if err != nil {
		return OutboxAction{}, fmt.Errorf("decode outbox request_payload_json for %s: %w", r.ActionID, err)
	}

	return OutboxAction{
		ActionID:           r.ActionID,
		EntityType:         r.EntityType,
		EntityID:           r.EntityID,
		ActionType:         r.ActionType,
		GitHubTargetType:   r.GitHubTargetType,
		GitHubTargetNumber: r.GitHubTargetNumber,
		IdempotencyKey:     r.IdempotencyKey,
		RequestPayloadJSON: requestPayload,
		Status:             r.Status,
		AttemptCount:       r.AttemptCount,
		LastError:          r.LastError,
		NextAttemptAt:      r.NextAttemptAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
		CompletedAt:        r.CompletedAt,
	}, nil
}

func (s BaseStore) requireDB() (bun.IDB, error) {
	if s.db == nil {
		return nil, ErrNotOpen
	}
	return s.db, nil
}

func decodeTaskStateStorePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return newValidationError("payload is required")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return newValidationError("decode payload: %v", err)
	}
	return nil
}

func taskStateStoreFailure(err error) TaskStateStoreResponse {
	return TaskStateStoreResponse{
		OK:    false,
		Error: asTaskStateStoreToolError(err),
	}
}

func asTaskStateStoreToolError(err error) *ToolError {
	if err == nil {
		return nil
	}

	var validationErr *validationError
	if errors.As(err, &validationErr) {
		return &ToolError{
			Code:      taskStateStoreErrorInvalidRequest,
			Message:   err.Error(),
			Retryable: false,
		}
	}

	var missingErr *notFoundError
	if errors.As(err, &missingErr) {
		return &ToolError{
			Code:      taskStateStoreErrorNotFound,
			Message:   err.Error(),
			Retryable: false,
		}
	}

	if errors.Is(err, ErrNotOpen) {
		return &ToolError{
			Code:      taskStateStoreErrorStoreNotOpen,
			Message:   err.Error(),
			Retryable: true,
		}
	}

	return &ToolError{
		Code:      taskStateStoreErrorInternal,
		Message:   err.Error(),
		Retryable: false,
	}
}

func validateAppendEventPayload(payload AppendEventPayload) error {
	if strings.TrimSpace(payload.EventID) == "" {
		return newValidationError("event_id is required")
	}
	if strings.TrimSpace(payload.EntityType) == "" {
		return newValidationError("entity_type is required")
	}
	if strings.TrimSpace(payload.EntityID) == "" {
		return newValidationError("entity_id is required")
	}
	if strings.TrimSpace(payload.EventType) == "" {
		return newValidationError("event_type is required")
	}
	if strings.TrimSpace(payload.Source) == "" {
		return newValidationError("source is required")
	}
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		return newValidationError("idempotency_key is required")
	}
	if err := validateUTCTimestamp("occurred_at", payload.OccurredAt); err != nil {
		return err
	}
	return nil
}

func validateUpdateTaskStatePayload(payload UpdateTaskStatePayload) error {
	if strings.TrimSpace(payload.TaskID) == "" {
		return newValidationError("task_id is required")
	}
	if strings.TrimSpace(payload.Status) == "" {
		return newValidationError("status is required")
	}
	return nil
}

func validateUpdateSprintStatePayload(payload UpdateSprintStatePayload) error {
	if strings.TrimSpace(payload.SprintID) == "" {
		return newValidationError("sprint_id is required")
	}
	if strings.TrimSpace(payload.Status) == "" {
		return newValidationError("status is required")
	}
	return nil
}

func validateEnqueueOutboxActionPayload(payload EnqueueOutboxActionPayload) error {
	if strings.TrimSpace(payload.ActionID) == "" {
		return newValidationError("action_id is required")
	}
	if strings.TrimSpace(payload.EntityType) == "" {
		return newValidationError("entity_type is required")
	}
	if strings.TrimSpace(payload.EntityID) == "" {
		return newValidationError("entity_id is required")
	}
	if strings.TrimSpace(payload.ActionType) == "" {
		return newValidationError("action_type is required")
	}
	if strings.TrimSpace(payload.GitHubTargetType) == "" {
		return newValidationError("github_target_type is required")
	}
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		return newValidationError("idempotency_key is required")
	}
	if payload.NextAttemptAt != nil {
		if err := validateUTCTimestamp("next_attempt_at", *payload.NextAttemptAt); err != nil {
			return err
		}
	}
	return nil
}

func validateUTCTimestamp(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return newValidationError("%s is required", field)
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return newValidationError("%s must be a UTC ISO 8601 string: %v", field, err)
	}

	_, offset := parsed.Zone()
	if offset != 0 {
		return newValidationError("%s must use UTC timezone", field)
	}
	return nil
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func marshalJSONObject(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}

	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal json object: %w", err)
	}
	return string(data), nil
}

func unmarshalJSONObject(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}

	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}

	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected JSON object")
	}
	return object, nil
}

func currentUTCTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newValidationError(format string, args ...any) error {
	return &validationError{
		message: fmt.Sprintf(format, args...),
	}
}
