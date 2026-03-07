package tasklist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"quick-ai-toolhub/internal/store"
)

type ProjectionStore interface {
	DB() bun.IDB
}

type Service struct {
	logger *slog.Logger
	store  ProjectionStore
}

type Dependencies struct {
	Logger *slog.Logger
	Store  ProjectionStore
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

type sprintRow struct {
	SprintID             string  `bun:"sprint_id"`
	SequenceNo           int     `bun:"sequence_no"`
	GitHubIssueNumber    int     `bun:"github_issue_number"`
	Status               string  `bun:"status"`
	SprintBranch         *string `bun:"sprint_branch"`
	ActiveSprintPRNumber *int    `bun:"active_sprint_pr_number"`
	NeedsHuman           bool    `bun:"needs_human"`
	HumanReason          *string `bun:"human_reason"`
}

type taskRow struct {
	TaskID            string  `bun:"task_id"`
	SprintID          string  `bun:"sprint_id"`
	TaskLocalID       string  `bun:"task_local_id"`
	SequenceNo        int     `bun:"sequence_no"`
	GitHubIssueNumber int     `bun:"github_issue_number"`
	Status            string  `bun:"status"`
	ActivePRNumber    *int    `bun:"active_pr_number"`
	TaskBranch        *string `bun:"task_branch"`
	WorktreePath      *string `bun:"worktree_path"`
	NeedsHuman        bool    `bun:"needs_human"`
	HumanReason       *string `bun:"human_reason"`
}

type dependencyRow struct {
	TaskID          string `bun:"task_id"`
	DependsOnTaskID string `bun:"depends_on_task_id"`
}

type syncStateRow struct {
	ValueJSON string `bun:"value_json"`
	UpdatedAt string `bun:"updated_at"`
}

type syncStateValue struct {
	SyncedAt string `json:"synced_at"`
}

func New(deps Dependencies) *Service {
	return &Service{
		logger: componentLogger(deps.Logger),
		store:  deps.Store,
	}
}

func (s *Service) Name() string {
	return "tasklist"
}

func (s *Service) Execute(ctx context.Context, req Request) Response {
	data, err := s.execute(ctx, req)
	if err != nil {
		return Response{
			OK:    false,
			Error: asToolError(err),
		}
	}

	return Response{
		OK:   true,
		Data: &data,
	}
}

func (s *Service) execute(ctx context.Context, req Request) (ResponseData, error) {
	if ctx == nil {
		return ResponseData{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return ResponseData{}, err
	}
	if err := validateRequest(req); err != nil {
		return ResponseData{}, err
	}
	if s.store == nil {
		return ResponseData{}, errors.New("projection store is required")
	}

	db := s.store.DB()
	if db == nil {
		return ResponseData{}, store.ErrNotOpen
	}

	sprints, err := loadSprints(ctx, db, req)
	if err != nil {
		return ResponseData{}, err
	}
	tasks, err := loadTasks(ctx, db, req)
	if err != nil {
		return ResponseData{}, err
	}
	if req.RefreshMode == RefreshModeTargeted && len(sprints) == 0 && len(tasks) == 0 {
		return ResponseData{}, &notFoundError{
			resource: "sprint",
			id:       strings.TrimSpace(req.SprintID),
		}
	}

	dependencies, err := loadDependencies(ctx, db, req)
	if err != nil {
		return ResponseData{}, err
	}
	refreshedAt, err := loadRefreshedAt(ctx, db, req)
	if err != nil {
		return ResponseData{}, err
	}

	return buildResponseData(req, sprints, tasks, dependencies, refreshedAt), nil
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "tasklist")
	}
	return logger.With("component", "tasklist")
}

func validateRequest(req Request) error {
	switch req.RefreshMode {
	case RefreshModeFull:
		if strings.TrimSpace(req.SprintID) != "" {
			return newValidationError("sprint_id is only supported when refresh_mode=targeted")
		}
	case RefreshModeTargeted:
		if strings.TrimSpace(req.SprintID) == "" {
			return newValidationError("sprint_id is required when refresh_mode=targeted")
		}
	case "":
		return newValidationError("refresh_mode is required")
	default:
		return newValidationError("unsupported refresh_mode %q", req.RefreshMode)
	}

	return nil
}

func loadSprints(ctx context.Context, db bun.IDB, req Request) ([]sprintRow, error) {
	var rows []sprintRow
	query := db.NewSelect().
		TableExpr("sprints").
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
		OrderExpr("sequence_no ASC")

	if req.RefreshMode == RefreshModeTargeted {
		query = query.Where("sprint_id = ?", strings.TrimSpace(req.SprintID))
	}

	if err := query.Scan(ctx, &rows); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load sprints: %w", err)
	}
	return rows, nil
}

func loadTasks(ctx context.Context, db bun.IDB, req Request) ([]taskRow, error) {
	var rows []taskRow
	query := db.NewSelect().
		TableExpr("tasks AS t").
		ColumnExpr("t.task_id").
		ColumnExpr("t.sprint_id").
		ColumnExpr("t.task_local_id").
		ColumnExpr("t.sequence_no").
		ColumnExpr("t.github_issue_number").
		ColumnExpr("t.status").
		ColumnExpr("t.active_pr_number").
		ColumnExpr("t.task_branch").
		ColumnExpr("t.worktree_path").
		ColumnExpr("t.needs_human").
		ColumnExpr("t.human_reason").
		Join("LEFT JOIN sprints AS s ON s.sprint_id = t.sprint_id").
		OrderExpr("CASE WHEN s.sequence_no IS NULL THEN 1 ELSE 0 END ASC").
		OrderExpr("s.sequence_no ASC").
		OrderExpr("t.sprint_id ASC").
		OrderExpr("t.sequence_no ASC")

	if req.RefreshMode == RefreshModeTargeted {
		query = query.Where("t.sprint_id = ?", strings.TrimSpace(req.SprintID))
	}

	if err := query.Scan(ctx, &rows); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load tasks: %w", err)
	}
	return rows, nil
}

func loadDependencies(ctx context.Context, db bun.IDB, req Request) ([]dependencyRow, error) {
	var rows []dependencyRow
	query := db.NewSelect().
		TableExpr("task_dependencies AS d").
		ColumnExpr("d.task_id").
		ColumnExpr("d.depends_on_task_id").
		Join("LEFT JOIN tasks AS src ON src.task_id = d.task_id").
		OrderExpr("d.task_id ASC").
		OrderExpr("d.depends_on_task_id ASC")

	if req.RefreshMode == RefreshModeTargeted {
		sprintID := strings.TrimSpace(req.SprintID)
		query = query.Where("(src.sprint_id = ? OR d.task_id LIKE ?)", sprintID, sprintID+"/%")
	}

	if err := query.Scan(ctx, &rows); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load task dependencies: %w", err)
	}
	return rows, nil
}

func loadRefreshedAt(ctx context.Context, db bun.IDB, req Request) (string, error) {
	var row syncStateRow
	err := db.NewSelect().
		TableExpr("sync_state").
		Column("value_json", "updated_at").
		Where("name = ?", "last_full_reconcile_at").
		Limit(1).
		Scan(ctx, &row)
	switch {
	case err == nil:
		value := syncStateValue{}
		if strings.TrimSpace(row.ValueJSON) != "" {
			if err := json.Unmarshal([]byte(row.ValueJSON), &value); err != nil {
				return "", fmt.Errorf("decode sync_state last_full_reconcile_at: %w", err)
			}
		}
		if refreshedAt := strings.TrimSpace(value.SyncedAt); refreshedAt != "" {
			return refreshedAt, nil
		}
		if refreshedAt := strings.TrimSpace(row.UpdatedAt); refreshedAt != "" {
			return refreshedAt, nil
		}
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("load sync_state last_full_reconcile_at: %w", err)
	}

	sprintTime, err := loadMaxProjectionTimestamp(ctx, db, "sprints", req)
	if err != nil {
		return "", err
	}
	taskTime, err := loadMaxProjectionTimestamp(ctx, db, "tasks", req)
	if err != nil {
		return "", err
	}

	refreshedAt := maxTimestamp(sprintTime, taskTime)
	if refreshedAt != "" {
		return refreshedAt, nil
	}
	return currentUTCTimestamp(), nil
}

func loadMaxProjectionTimestamp(ctx context.Context, db bun.IDB, table string, req Request) (string, error) {
	if table != "sprints" && table != "tasks" {
		return "", newValidationError("unsupported projection table %q", table)
	}

	query := db.NewSelect().
		TableExpr(table).
		ColumnExpr("MAX(COALESCE(last_issue_sync_at, updated_at))")

	if req.RefreshMode == RefreshModeTargeted {
		query = query.Where("sprint_id = ?", strings.TrimSpace(req.SprintID))
	}

	var value *string
	if err := query.Scan(ctx, &value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("load max projection timestamp from %s: %w", table, err)
	}
	if value == nil {
		return "", nil
	}
	return strings.TrimSpace(*value), nil
}

func buildResponseData(
	req Request,
	sprints []sprintRow,
	tasks []taskRow,
	dependencies []dependencyRow,
	refreshedAt string,
) ResponseData {
	sprintProjections := make([]store.SprintProjection, 0, len(sprints))
	sprintByID := make(map[string]store.SprintProjection, len(sprints))
	tasksBySprint := make(map[string][]taskRow, len(sprints))
	taskByID := make(map[string]taskRow, len(tasks))

	for _, sprint := range sprints {
		projection := sprint.toProjection()
		sprintProjections = append(sprintProjections, projection)
		sprintByID[projection.SprintID] = projection
	}
	for _, task := range tasks {
		taskByID[task.TaskID] = task
		if _, ok := sprintByID[task.SprintID]; ok {
			tasksBySprint[task.SprintID] = append(tasksBySprint[task.SprintID], task)
		}
	}

	depsByTask := make(map[string][]dependencyRow, len(dependencies))
	issues := newBlockingIssueCollector()
	for _, dependency := range dependencies {
		depsByTask[dependency.TaskID] = append(depsByTask[dependency.TaskID], dependency)
		if _, ok := taskByID[dependency.TaskID]; !ok {
			issues.add("task", dependency.TaskID, "dependency source task is missing from local projection")
		}
	}

	if len(sprints) == 0 && len(tasks) == 0 {
		issues.add("repo", "repo", "no sprint or task projections are available")
	}

	entries := make([]TaskEntry, 0, len(tasks))

	// Only the earliest unfinished task in a sprint can move forward.
	for _, sprint := range sprints {
		projectedTasks := tasksBySprint[sprint.SprintID]
		if len(projectedTasks) == 0 {
			issues.add("sprint", sprint.SprintID, "sprint has no projected tasks")
		}
		if sprint.NeedsHuman {
			issues.add("sprint", sprint.SprintID, humanBlockReason("sprint", sprint.HumanReason))
		}

		priorUnfinishedTaskID := ""
		for _, task := range projectedTasks {
			reasons := make([]string, 0, 8)

			if sprint.NeedsHuman {
				reasons = append(reasons, humanBlockReason("sprint", sprint.HumanReason))
			}
			if reason := sprintStatusBlockReason(sprint.Status); reason != "" {
				reasons = append(reasons, reason)
			}
			if task.NeedsHuman {
				reason := humanBlockReason("task", task.HumanReason)
				reasons = append(reasons, reason)
				issues.add("task", task.TaskID, reason)
			}
			if reason := taskStatusBlockReason(task.Status); reason != "" {
				reasons = append(reasons, reason)
			}
			if priorUnfinishedTaskID != "" && priorUnfinishedTaskID != task.TaskID {
				reasons = append(reasons, fmt.Sprintf("waiting for prior task %s to finish", priorUnfinishedTaskID))
			}
			reasons = append(reasons, dependencyBlockReasons(task, depsByTask[task.TaskID], taskByID, issues)...)

			entries = append(entries, TaskEntry{
				Task:      task.toProjection(),
				BlockedBy: dedupeStrings(reasons),
			})

			if task.Status != "done" && priorUnfinishedTaskID == "" {
				priorUnfinishedTaskID = task.TaskID
			}
		}
	}

	for _, task := range tasks {
		if _, ok := sprintByID[task.SprintID]; ok {
			continue
		}

		reasons := []string{
			fmt.Sprintf("task is orphaned in local projection; sprint %s is not projected", task.SprintID),
		}
		issues.add("task", task.TaskID, reasons[0])
		if task.NeedsHuman {
			reason := humanBlockReason("task", task.HumanReason)
			reasons = append(reasons, reason)
			issues.add("task", task.TaskID, reason)
		}
		if reason := taskStatusBlockReason(task.Status); reason != "" {
			reasons = append(reasons, reason)
		}
		reasons = append(reasons, dependencyBlockReasons(task, depsByTask[task.TaskID], taskByID, issues)...)

		entries = append(entries, TaskEntry{
			Task:      task.toProjection(),
			BlockedBy: dedupeStrings(reasons),
		})
	}

	return ResponseData{
		Sprints: sprintProjections,
		Tasks:   entries,
		SyncSummary: SyncSummary{
			Mode:        string(req.RefreshMode),
			RefreshedAt: refreshedAt,
			SprintCount: len(sprintProjections),
			TaskCount:   len(entries),
		},
		BlockingIssues: issues.list(),
	}
}

func dependencyBlockReasons(
	task taskRow,
	dependencies []dependencyRow,
	taskByID map[string]taskRow,
	issues *blockingIssueCollector,
) []string {
	if len(dependencies) == 0 {
		return nil
	}

	reasons := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		dependencyTask, ok := taskByID[dependency.DependsOnTaskID]
		if !ok {
			reason := fmt.Sprintf("dependency target %s is missing from local projection", dependency.DependsOnTaskID)
			reasons = append(reasons, reason)
			issues.add("task", task.TaskID, reason)
			continue
		}
		if dependencyTask.SprintID != task.SprintID {
			reason := fmt.Sprintf("task has cross-sprint dependency on %s", dependencyTask.TaskID)
			reasons = append(reasons, reason)
			issues.add("task", task.TaskID, reason)
			continue
		}
		if dependencyTask.Status != "done" {
			reason := fmt.Sprintf("waiting for dependency %s to finish", dependencyTask.TaskID)
			reasons = append(reasons, reason)
			issues.add("task", task.TaskID, reason)
		}
	}

	return reasons
}

func humanBlockReason(scope string, reason *string) string {
	prefix := scope + " requires human intervention"
	if trimmed := strings.TrimSpace(optionalString(reason)); trimmed != "" {
		return prefix + ": " + trimmed
	}
	return prefix
}

func sprintStatusBlockReason(status string) string {
	switch strings.TrimSpace(status) {
	case "todo", "in_progress", "partially_done":
		return ""
	case "done":
		return "sprint is already completed"
	case "":
		return "sprint status is empty and not schedulable"
	default:
		return fmt.Sprintf("sprint status %s is not accepting new tasks", status)
	}
}

func taskStatusBlockReason(status string) string {
	switch strings.TrimSpace(status) {
	case "todo", "in_progress":
		return ""
	case "done":
		return "task is already completed"
	case "":
		return "task status is empty and not schedulable"
	default:
		return fmt.Sprintf("task status %s is not schedulable", status)
	}
}

func (r sprintRow) toProjection() store.SprintProjection {
	return store.SprintProjection{
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

func (r taskRow) toProjection() store.TaskProjection {
	return store.TaskProjection{
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

type blockingIssueCollector struct {
	values map[string]BlockingIssue
}

func newBlockingIssueCollector() *blockingIssueCollector {
	return &blockingIssueCollector{
		values: make(map[string]BlockingIssue),
	}
}

func (c *blockingIssueCollector) add(scope, entityID, reason string) {
	scope = strings.TrimSpace(scope)
	entityID = strings.TrimSpace(entityID)
	reason = strings.TrimSpace(reason)
	if scope == "" || entityID == "" || reason == "" {
		return
	}

	key := scope + "\x00" + entityID + "\x00" + reason
	c.values[key] = BlockingIssue{
		Scope:    scope,
		EntityID: entityID,
		Reason:   reason,
	}
}

func (c *blockingIssueCollector) list() []BlockingIssue {
	if len(c.values) == 0 {
		return []BlockingIssue{}
	}

	issues := make([]BlockingIssue, 0, len(c.values))
	for _, issue := range c.values {
		issues = append(issues, issue)
	}

	sort.Slice(issues, func(i, j int) bool {
		leftOrder := blockingIssueScopeOrder(issues[i].Scope)
		rightOrder := blockingIssueScopeOrder(issues[j].Scope)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if issues[i].EntityID != issues[j].EntityID {
			return issues[i].EntityID < issues[j].EntityID
		}
		return issues[i].Reason < issues[j].Reason
	})

	return issues
}

func blockingIssueScopeOrder(scope string) int {
	switch scope {
	case "repo":
		return 0
	case "sprint":
		return 1
	case "task":
		return 2
	default:
		return 3
	}
}

func maxTimestamp(values ...string) string {
	maxValue := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if maxValue == "" || value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func asToolError(err error) *ToolError {
	if err == nil {
		return nil
	}

	var validationErr *validationError
	if errors.As(err, &validationErr) {
		return &ToolError{
			Code:      ErrorCodeInvalid,
			Message:   validationErr.Error(),
			Retryable: false,
		}
	}

	var missingErr *notFoundError
	if errors.As(err, &missingErr) {
		return &ToolError{
			Code:      ErrorCodeNotFound,
			Message:   missingErr.Error(),
			Retryable: false,
		}
	}

	return &ToolError{
		Code:      ErrorCodeInternal,
		Message:   err.Error(),
		Retryable: false,
	}
}

func newValidationError(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}

func currentUTCTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
