package leader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/worktreeprep"
)

const (
	eventTaskSelected      = "task_selected"
	eventTaskStarted       = "task_started"
	eventSprintInitialized = "sprint_initialized"
)

type StartedWorkItemEvents struct {
	TaskSelected      *store.AppendEventResult `json:"task_selected,omitempty"`
	SprintInitialized *store.AppendEventResult `json:"sprint_initialized,omitempty"`
	TaskStarted       *store.AppendEventResult `json:"task_started,omitempty"`
}

type StartedWorkItemResult struct {
	Status         SelectionStatus            `json:"status"`
	Sprint         *store.SprintProjection    `json:"sprint,omitempty"`
	Task           *store.TaskProjection      `json:"task,omitempty"`
	Worktree       *worktreeprep.ResponseData `json:"worktree,omitempty"`
	Reason         string                     `json:"reason,omitempty"`
	BlockingIssues []tasklist.BlockingIssue   `json:"blocking_issues"`
	Attempt        int                        `json:"attempt,omitempty"`
	Resumed        bool                       `json:"resumed"`
	Events         StartedWorkItemEvents      `json:"events,omitempty"`
}

type taskAttemptSnapshot struct {
	AttemptTotal int `bun:"attempt_total"`
}

func (s *Service) StartNextWorkItem(ctx context.Context, opts PrepareNextWorkItemOptions) (StartedWorkItemResult, error) {
	selection, err := s.SelectNextWorkItem(ctx)
	if err != nil {
		return StartedWorkItemResult{}, err
	}
	if selection.Status != SelectionStatusSelected || selection.Sprint == nil || selection.Task == nil {
		prepared := preparedWorkItemResultFromSelection(selection)
		return startedWorkItemResultFromPrepared(prepared), nil
	}
	unlock := s.lockTaskStartup(selection.Task.TaskID)
	defer unlock()

	prepared, err := s.prepareSelectedWorkItem(ctx, selection, opts)
	if err != nil {
		return StartedWorkItemResult{}, err
	}
	return s.startPreparedWorkItem(ctx, prepared, false)
}

func (s *Service) StartPreparedWorkItem(ctx context.Context, prepared PreparedWorkItemResult) (StartedWorkItemResult, error) {
	return s.startPreparedWorkItem(ctx, prepared, true)
}

func (s *Service) startPreparedWorkItem(ctx context.Context, prepared PreparedWorkItemResult, lockTask bool) (StartedWorkItemResult, error) {
	if ctx == nil {
		return StartedWorkItemResult{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return StartedWorkItemResult{}, err
	}

	result := startedWorkItemResultFromPrepared(prepared)
	if err := validatePreparedWorkItemStartInput(prepared); err != nil {
		return StartedWorkItemResult{}, err
	}
	if s.store == nil {
		return StartedWorkItemResult{}, errors.New("store is required")
	}
	if lockTask {
		unlock := s.lockTaskStartup(prepared.Task.TaskID)
		defer unlock()
	}

	var (
		updatedSprint store.SprintProjection
		updatedTask   store.TaskProjection
		events        StartedWorkItemEvents
		attempt       int
		resumed       bool
	)

	err := s.store.RunInTx(ctx, func(ctx context.Context, tx store.BaseStore) error {
		currentSprint, err := tx.LoadSprintProjection(ctx, prepared.Sprint.SprintID)
		if err != nil {
			return err
		}
		currentTask, err := tx.LoadTaskProjection(ctx, prepared.Task.TaskID)
		if err != nil {
			return err
		}
		currentAttempt, err := loadTaskAttemptTotal(ctx, tx.DB(), prepared.Task.TaskID)
		if err != nil {
			return err
		}

		if err := validateStartPreparedWorkItem(currentSprint, currentTask, prepared); err != nil {
			return err
		}

		attempt = startupAttempt(currentTask.Status, currentAttempt)
		resumed = strings.TrimSpace(currentTask.Status) != "todo"

		taskSelected, err := tx.AppendEvent(ctx, buildTaskSelectedEvent(currentSprint, currentTask, prepared, attempt))
		if err != nil {
			return err
		}
		events.TaskSelected = &taskSelected

		targetSprintStatus := startedSprintStatus(currentSprint.Status)
		if strings.TrimSpace(currentSprint.Status) == "todo" {
			sprintInitialized, err := tx.AppendEvent(ctx, buildSprintInitializedEvent(currentSprint, currentTask, prepared, attempt))
			if err != nil {
				return err
			}
			events.SprintInitialized = &sprintInitialized
		}

		sprintUpdate := store.UpdateSprintStatePayload{
			SprintID:     currentSprint.SprintID,
			Status:       targetSprintStatus,
			SprintBranch: stringPointer(prepared.Worktree.BaseBranch),
		}
		if shouldUpdateSprintProjectionForStart(currentSprint, sprintUpdate) {
			updatedSprint, err = tx.UpdateSprintState(ctx, sprintUpdate)
			if err != nil {
				return err
			}
		} else {
			updatedSprint = currentSprint
		}

		taskStarted, err := tx.AppendEvent(ctx, buildTaskStartedEvent(currentSprint, currentTask, prepared, attempt))
		if err != nil {
			return err
		}
		events.TaskStarted = &taskStarted

		taskUpdate := store.UpdateTaskStatePayload{
			TaskID:       currentTask.TaskID,
			Status:       startedTaskStatus(currentTask.Status),
			AttemptTotal: intPointer(attempt),
			TaskBranch:   stringPointer(prepared.Worktree.TaskBranch),
			WorktreePath: stringPointer(prepared.Worktree.WorktreePath),
		}
		if shouldUpdateTaskProjectionForStart(currentTask, currentAttempt, taskUpdate) {
			updatedTask, err = tx.UpdateTaskState(ctx, taskUpdate)
			if err != nil {
				return err
			}
		} else {
			updatedTask = currentTask
		}

		return nil
	})
	if err != nil {
		return StartedWorkItemResult{}, err
	}

	result.Sprint = copySprintProjection(&updatedSprint)
	result.Task = copyTaskProjection(&updatedTask)
	result.Attempt = attempt
	result.Resumed = resumed
	result.Events = events
	return result, nil
}

func startedWorkItemResultFromPrepared(prepared PreparedWorkItemResult) StartedWorkItemResult {
	return StartedWorkItemResult{
		Status:         prepared.Status,
		Sprint:         copySprintProjection(prepared.Sprint),
		Task:           copyTaskProjection(prepared.Task),
		Worktree:       copyWorktreeData(prepared.Worktree),
		Reason:         prepared.Reason,
		BlockingIssues: copyBlockingIssues(prepared.BlockingIssues),
	}
}

func validatePreparedWorkItemStartInput(prepared PreparedWorkItemResult) error {
	if prepared.Status != SelectionStatusSelected {
		return fmt.Errorf("prepared work item status must be %s, got %s", SelectionStatusSelected, prepared.Status)
	}
	if prepared.Sprint == nil {
		return errors.New("prepared sprint is required")
	}
	if prepared.Task == nil {
		return errors.New("prepared task is required")
	}
	if prepared.Worktree == nil {
		return errors.New("prepared worktree data is required")
	}
	worktreePath := strings.TrimSpace(prepared.Worktree.WorktreePath)
	if worktreePath == "" {
		return errors.New("prepared worktree path is required")
	}
	if !filepath.IsAbs(worktreePath) {
		return errors.New("prepared worktree path must be absolute")
	}
	worktreeInfo, err := os.Stat(worktreePath)
	if err != nil {
		return fmt.Errorf("prepared worktree path %s: %w", worktreePath, err)
	}
	if !worktreeInfo.IsDir() {
		return fmt.Errorf("prepared worktree path %s must be a directory", worktreePath)
	}
	taskBranch := strings.TrimSpace(prepared.Worktree.TaskBranch)
	if taskBranch == "" {
		return errors.New("prepared task branch is required")
	}
	expectedTaskBranch := "task/" + prepared.Task.TaskID
	if taskBranch != expectedTaskBranch {
		return fmt.Errorf("prepared task branch must be %s", expectedTaskBranch)
	}
	baseBranch := strings.TrimSpace(prepared.Worktree.BaseBranch)
	if baseBranch == "" {
		return errors.New("prepared sprint branch is required")
	}
	expectedSprintBranch := "sprint/" + prepared.Sprint.SprintID
	if baseBranch != expectedSprintBranch {
		return fmt.Errorf("prepared sprint branch must be %s", expectedSprintBranch)
	}
	if strings.TrimSpace(prepared.Worktree.BaseCommitSHA) == "" {
		return errors.New("prepared base commit sha is required")
	}
	return nil
}

func shouldUpdateSprintProjectionForStart(current store.SprintProjection, payload store.UpdateSprintStatePayload) bool {
	if strings.TrimSpace(current.Status) != strings.TrimSpace(payload.Status) {
		return true
	}
	if payload.SprintBranch != nil && strings.TrimSpace(optionalString(current.SprintBranch)) != strings.TrimSpace(optionalString(payload.SprintBranch)) {
		return true
	}
	return false
}

func shouldUpdateTaskProjectionForStart(current store.TaskProjection, currentAttempt int, payload store.UpdateTaskStatePayload) bool {
	if strings.TrimSpace(current.Status) != strings.TrimSpace(payload.Status) {
		return true
	}
	if payload.AttemptTotal != nil && currentAttempt != *payload.AttemptTotal {
		return true
	}
	if payload.TaskBranch != nil && strings.TrimSpace(optionalString(current.TaskBranch)) != strings.TrimSpace(optionalString(payload.TaskBranch)) {
		return true
	}
	if payload.WorktreePath != nil && strings.TrimSpace(optionalString(current.WorktreePath)) != strings.TrimSpace(optionalString(payload.WorktreePath)) {
		return true
	}
	return false
}

func buildTaskSelectedEvent(
	currentSprint store.SprintProjection,
	currentTask store.TaskProjection,
	prepared PreparedWorkItemResult,
	attempt int,
) store.AppendEventPayload {
	return store.AppendEventPayload{
		EventID:        taskEventID(prepared.Task.TaskID, eventTaskSelected, attempt),
		EntityType:     "task",
		EntityID:       prepared.Task.TaskID,
		SprintID:       stringPointer(prepared.Sprint.SprintID),
		TaskID:         stringPointer(prepared.Task.TaskID),
		EventType:      eventTaskSelected,
		Source:         "leader",
		Attempt:        intPointer(attempt),
		IdempotencyKey: taskEventIdempotencyKey(prepared.Task.TaskID, eventTaskSelected, attempt),
		PayloadJSON: map[string]any{
			"task_status_from":   strings.TrimSpace(currentTask.Status),
			"sprint_status_from": strings.TrimSpace(currentSprint.Status),
			"task_branch":        prepared.Worktree.TaskBranch,
			"sprint_branch":      prepared.Worktree.BaseBranch,
			"worktree_path":      prepared.Worktree.WorktreePath,
			"base_commit_sha":    prepared.Worktree.BaseCommitSHA,
			"worktree_reused":    prepared.Worktree.Reused,
		},
		OccurredAt: currentUTCTimestamp(),
	}
}

func buildSprintInitializedEvent(
	currentSprint store.SprintProjection,
	currentTask store.TaskProjection,
	prepared PreparedWorkItemResult,
	attempt int,
) store.AppendEventPayload {
	return store.AppendEventPayload{
		EventID:        sprintInitializedEventID(prepared.Sprint.SprintID),
		EntityType:     "sprint",
		EntityID:       prepared.Sprint.SprintID,
		SprintID:       stringPointer(prepared.Sprint.SprintID),
		TaskID:         stringPointer(prepared.Task.TaskID),
		EventType:      eventSprintInitialized,
		Source:         "leader",
		Attempt:        intPointer(attempt),
		IdempotencyKey: sprintInitializedIdempotencyKey(prepared.Sprint.SprintID),
		PayloadJSON: map[string]any{
			"sprint_status_from": strings.TrimSpace(currentSprint.Status),
			"sprint_status_to":   "in_progress",
			"sprint_branch":      prepared.Worktree.BaseBranch,
			"trigger_task_id":    currentTask.TaskID,
		},
		OccurredAt: currentUTCTimestamp(),
	}
}

func buildTaskStartedEvent(
	currentSprint store.SprintProjection,
	currentTask store.TaskProjection,
	prepared PreparedWorkItemResult,
	attempt int,
) store.AppendEventPayload {
	return store.AppendEventPayload{
		EventID:        taskEventID(prepared.Task.TaskID, eventTaskStarted, attempt),
		EntityType:     "task",
		EntityID:       prepared.Task.TaskID,
		SprintID:       stringPointer(prepared.Sprint.SprintID),
		TaskID:         stringPointer(prepared.Task.TaskID),
		EventType:      eventTaskStarted,
		Source:         "leader",
		Attempt:        intPointer(attempt),
		IdempotencyKey: taskEventIdempotencyKey(prepared.Task.TaskID, eventTaskStarted, attempt),
		PayloadJSON: map[string]any{
			"task_status_from": strings.TrimSpace(currentTask.Status),
			"task_status_to":   "in_progress",
			"sprint_status":    strings.TrimSpace(startedSprintStatus(currentSprint.Status)),
			"task_branch":      prepared.Worktree.TaskBranch,
			"sprint_branch":    prepared.Worktree.BaseBranch,
			"worktree_path":    prepared.Worktree.WorktreePath,
			"base_commit_sha":  prepared.Worktree.BaseCommitSHA,
			"worktree_reused":  prepared.Worktree.Reused,
		},
		OccurredAt: currentUTCTimestamp(),
	}
}

func validateStartPreparedWorkItem(
	currentSprint store.SprintProjection,
	currentTask store.TaskProjection,
	prepared PreparedWorkItemResult,
) error {
	if err := validateSprintStartStatus(currentSprint.Status, currentSprint.SprintID); err != nil {
		return err
	}
	if err := validateTaskStartStatus(currentTask.Status, currentTask.TaskID); err != nil {
		return err
	}
	if currentTask.SprintID != currentSprint.SprintID {
		return fmt.Errorf("task %s is projected under sprint %s, not %s", currentTask.TaskID, currentTask.SprintID, currentSprint.SprintID)
	}
	if err := validateReferenceMatch("sprint_branch", currentSprint.SprintBranch, prepared.Worktree.BaseBranch); err != nil {
		return err
	}
	if err := validateReferenceMatch("task_branch", currentTask.TaskBranch, prepared.Worktree.TaskBranch); err != nil {
		return err
	}
	if err := validateWorktreePathMatch(currentTask.WorktreePath, prepared.Worktree.WorktreePath); err != nil {
		return err
	}
	return nil
}

func validateSprintStartStatus(status, sprintID string) error {
	switch strings.TrimSpace(status) {
	case "todo", "in_progress", "partially_done":
		return nil
	default:
		return fmt.Errorf("sprint %s status %s cannot accept task startup", sprintID, strings.TrimSpace(status))
	}
}

func validateTaskStartStatus(status, taskID string) error {
	switch strings.TrimSpace(status) {
	case "todo", "in_progress":
		return nil
	default:
		return fmt.Errorf("task %s status %s cannot be started", taskID, strings.TrimSpace(status))
	}
}

func validateReferenceMatch(name string, current *string, expected string) error {
	currentValue := strings.TrimSpace(optionalString(current))
	expected = strings.TrimSpace(expected)
	if currentValue == "" || expected == "" || currentValue == expected {
		return nil
	}
	return fmt.Errorf("%s is already set to %s and cannot be replaced with %s", name, currentValue, expected)
}

func validateWorktreePathMatch(current *string, expected string) error {
	currentValue := strings.TrimSpace(optionalString(current))
	expected = strings.TrimSpace(expected)
	if currentValue == "" || expected == "" || currentValue == expected {
		return nil
	}
	if _, err := os.Stat(currentValue); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat current worktree path %s: %w", currentValue, err)
	}
	if currentValue == expected {
		return nil
	}
	return fmt.Errorf("worktree_path is already set to %s and cannot be replaced with %s while the current path is still accessible", currentValue, expected)
}

func loadTaskAttemptTotal(ctx context.Context, db bun.IDB, taskID string) (int, error) {
	var snapshot taskAttemptSnapshot
	if err := db.NewSelect().
		TableExpr("tasks").
		Column("attempt_total").
		Where("task_id = ?", strings.TrimSpace(taskID)).
		Limit(1).
		Scan(ctx, &snapshot); err != nil {
		return 0, fmt.Errorf("load attempt_total for task %s: %w", taskID, err)
	}
	return snapshot.AttemptTotal, nil
}

func startupAttempt(taskStatus string, currentAttempt int) int {
	if strings.TrimSpace(taskStatus) == "todo" {
		return currentAttempt + 1
	}
	if currentAttempt > 0 {
		return currentAttempt
	}
	return 1
}

func startedSprintStatus(currentStatus string) string {
	if strings.TrimSpace(currentStatus) == "todo" {
		return "in_progress"
	}
	return strings.TrimSpace(currentStatus)
}

func startedTaskStatus(currentStatus string) string {
	if strings.TrimSpace(currentStatus) == "todo" {
		return "in_progress"
	}
	return strings.TrimSpace(currentStatus)
}

func taskEventID(taskID, eventType string, attempt int) string {
	return fmt.Sprintf("evt_%s_%s_%02d", normalizeEventEntityID(taskID), eventType, attempt)
}

func sprintInitializedEventID(sprintID string) string {
	return "evt_" + normalizeEventEntityID(sprintID) + "_" + eventSprintInitialized
}

func taskEventIdempotencyKey(taskID, eventType string, attempt int) string {
	return fmt.Sprintf("leader:%s:%s:%02d", strings.TrimSpace(taskID), eventType, attempt)
}

func sprintInitializedIdempotencyKey(sprintID string) string {
	return "leader:" + strings.TrimSpace(sprintID) + ":" + eventSprintInitialized
}

func normalizeEventEntityID(value string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

func currentUTCTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func copyWorktreeData(value *worktreeprep.ResponseData) *worktreeprep.ResponseData {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func intPointer(value int) *int {
	return &value
}
