package tasklist

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uptrace/bun/driver/sqliteshim"

	"quick-ai-toolhub/internal/store"
)

func TestExecuteOrdersByBusinessSequenceAndComputesBlocking(t *testing.T) {
	base, service, _ := openTestStore(t)

	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-10",
		SequenceNo:        10,
		GitHubIssueNumber: 110,
		Status:            "todo",
	})
	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-02",
		SequenceNo:        2,
		GitHubIssueNumber: 102,
		Status:            "in_progress",
	})

	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-03",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-03",
		SequenceNo:              3,
		GitHubIssueNumber:       203,
		ParentGitHubIssueNumber: 102,
		Status:                  "todo",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-01",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       201,
		ParentGitHubIssueNumber: 102,
		Status:                  "done",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-10/Task-01",
		SprintID:                "Sprint-10",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       301,
		ParentGitHubIssueNumber: 110,
		Status:                  "todo",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-02",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       202,
		ParentGitHubIssueNumber: 102,
		Status:                  "todo",
	})

	insertDependencyRow(t, service, "Sprint-02/Task-03", "Sprint-02/Task-02")

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeFull,
	})
	if !response.OK {
		t.Fatalf("expected task list to load, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}

	if got := sprintIDs(response.Data.Sprints); strings.Join(got, ",") != "Sprint-02,Sprint-10" {
		t.Fatalf("unexpected sprint order: %v", got)
	}
	if got := taskIDs(response.Data.Tasks); strings.Join(got, ",") != "Sprint-02/Task-01,Sprint-02/Task-02,Sprint-02/Task-03,Sprint-10/Task-01" {
		t.Fatalf("unexpected task order: %v", got)
	}

	task02 := response.Data.Tasks[1]
	if len(task02.BlockedBy) != 0 {
		t.Fatalf("expected Sprint-02/Task-02 to be schedulable, got %v", task02.BlockedBy)
	}

	task03 := response.Data.Tasks[2]
	assertContainsReason(t, task03.BlockedBy, "waiting for prior task Sprint-02/Task-02 to finish")
	assertContainsReason(t, task03.BlockedBy, "waiting for dependency Sprint-02/Task-02 to finish")

	if response.Data.SyncSummary.Mode != "full" {
		t.Fatalf("unexpected sync mode: %s", response.Data.SyncSummary.Mode)
	}
	if response.Data.SyncSummary.SprintCount != 2 || response.Data.SyncSummary.TaskCount != 4 {
		t.Fatalf("unexpected sync counts: %+v", response.Data.SyncSummary)
	}
	found := false
	for _, issue := range response.Data.BlockingIssues {
		if issue.Scope == "task" && issue.EntityID == "Sprint-02/Task-03" &&
			issue.Reason == "waiting for dependency Sprint-02/Task-02 to finish" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dependency blocking issue, got %+v", response.Data.BlockingIssues)
	}
}

func TestExecuteTreatsInProgressTaskAsSchedulableForRecovery(t *testing.T) {
	base, service, _ := openTestStore(t)

	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 103,
		Status:            "in_progress",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-03/Task-01",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       301,
		ParentGitHubIssueNumber: 103,
		Status:                  "in_progress",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-03/Task-02",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-02",
		SequenceNo:              2,
		GitHubIssueNumber:       302,
		ParentGitHubIssueNumber: 103,
		Status:                  "todo",
	})

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeTargeted,
		SprintID:    "Sprint-03",
	})
	if !response.OK {
		t.Fatalf("expected task list to load, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if len(response.Data.Tasks) != 2 {
		t.Fatalf("expected two tasks, got %d", len(response.Data.Tasks))
	}

	inProgressTask := response.Data.Tasks[0]
	if inProgressTask.Task.TaskID != "Sprint-03/Task-01" {
		t.Fatalf("unexpected first task: %+v", inProgressTask.Task)
	}
	if len(inProgressTask.BlockedBy) != 0 {
		t.Fatalf("expected in-progress task to stay schedulable for recovery, got %v", inProgressTask.BlockedBy)
	}

	nextTask := response.Data.Tasks[1]
	assertContainsReason(t, nextTask.BlockedBy, "waiting for prior task Sprint-03/Task-01 to finish")
}

func TestExecuteSurfacesOrphanTaskBlockingIssue(t *testing.T) {
	base, _, dbPath := openTestStore(t)

	insertCorruptTaskRow(t, dbPath, taskSeed{
		TaskID:                  "Sprint-99/Task-01",
		SprintID:                "Sprint-99",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       9901,
		ParentGitHubIssueNumber: 999,
		Status:                  "todo",
	})

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeFull,
	})
	if !response.OK {
		t.Fatalf("expected orphan projection to be reported in data, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if len(response.Data.Tasks) != 1 {
		t.Fatalf("expected one projected task, got %d", len(response.Data.Tasks))
	}

	assertContainsReason(t, response.Data.Tasks[0].BlockedBy, "task is orphaned in local projection; sprint Sprint-99 is not projected")

	found := false
	for _, issue := range response.Data.BlockingIssues {
		if issue.Scope == "task" && issue.EntityID == "Sprint-99/Task-01" &&
			strings.Contains(issue.Reason, "orphaned in local projection") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected orphan task blocking issue, got %+v", response.Data.BlockingIssues)
	}
}

func TestExecuteRejectsTargetedModeWithoutSprintID(t *testing.T) {
	base, _, _ := openTestStore(t)

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeTargeted,
	})
	if response.OK {
		t.Fatal("expected invalid request to fail")
	}
	if response.Error == nil {
		t.Fatal("expected tool error")
	}
	if response.Error.Code != ErrorCodeInvalid {
		t.Fatalf("unexpected error code: %s", response.Error.Code)
	}
	if !strings.Contains(response.Error.Message, "sprint_id is required") {
		t.Fatalf("unexpected error message: %s", response.Error.Message)
	}
}

func TestExecuteTargetedModeReportsMissingDependencySource(t *testing.T) {
	base, service, dbPath := openTestStore(t)

	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-02",
		SequenceNo:        2,
		GitHubIssueNumber: 102,
		Status:            "in_progress",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-01",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       201,
		ParentGitHubIssueNumber: 102,
		Status:                  "todo",
	})
	insertCorruptDependencyRow(t, dbPath, "Sprint-02/Task-02", "Sprint-02/Task-01")

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeTargeted,
		SprintID:    "Sprint-02",
	})
	if !response.OK {
		t.Fatalf("expected targeted task list to load, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if len(response.Data.Sprints) != 1 {
		t.Fatalf("expected one sprint, got %d", len(response.Data.Sprints))
	}
	if len(response.Data.Tasks) != 1 {
		t.Fatalf("expected one projected task, got %d", len(response.Data.Tasks))
	}

	found := false
	for _, issue := range response.Data.BlockingIssues {
		if issue.Scope == "task" && issue.EntityID == "Sprint-02/Task-02" &&
			issue.Reason == "dependency source task is missing from local projection" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing dependency source issue, got %+v", response.Data.BlockingIssues)
	}
}

func TestExecuteTargetedModeIgnoresOtherSprintDependencyPrefixMatches(t *testing.T) {
	base, service, dbPath := openTestStore(t)

	insertSprintRow(t, service, sprintSeed{
		SprintID:          "Sprint-02",
		SequenceNo:        2,
		GitHubIssueNumber: 102,
		Status:            "in_progress",
	})
	insertTaskRow(t, service, taskSeed{
		TaskID:                  "Sprint-02/Task-01",
		SprintID:                "Sprint-02",
		TaskLocalID:             "Task-01",
		SequenceNo:              1,
		GitHubIssueNumber:       201,
		ParentGitHubIssueNumber: 102,
		Status:                  "todo",
	})
	insertCorruptDependencyRow(t, dbPath, "Sprint-020/Task-09", "Sprint-020/Task-01")

	response := New(Dependencies{Store: base}).Execute(context.Background(), Request{
		RefreshMode: RefreshModeTargeted,
		SprintID:    "Sprint-02",
	})
	if !response.OK {
		t.Fatalf("expected targeted task list to load, got %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if got := taskIDs(response.Data.Tasks); strings.Join(got, ",") != "Sprint-02/Task-01" {
		t.Fatalf("unexpected targeted task order: %v", got)
	}

	for _, issue := range response.Data.BlockingIssues {
		if issue.EntityID == "Sprint-020/Task-09" {
			t.Fatalf("expected Sprint-020 dependency rows to be excluded, got %+v", response.Data.BlockingIssues)
		}
	}
}

func openTestStore(t *testing.T) (store.BaseStore, *store.Service, string) {
	t.Helper()

	repoRoot := newTestRepoRoot(t)
	dbPath := filepath.Join(repoRoot, ".toolhub", "toolhub.db")

	service := store.New(store.Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), store.OpenOptions{
		ConfigPath:   filepath.Join(repoRoot, "config", "config.yaml"),
		DatabasePath: ".toolhub/toolhub.db",
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}

	base, err := service.BaseStore()
	if err != nil {
		t.Fatalf("base store: %v", err)
	}
	return base, service, dbPath
}

func newTestRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeTestFile(t, root, filepath.Join("config", "config.yaml"), "store: test\n")
	writeTestFile(t, root, filepath.Join("sql", "schema.sql"), loadRepoSchema(t))
	return root
}

func loadRepoSchema(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	schemaPath := filepath.Join(filepath.Dir(file), "..", "..", "sql", "schema.sql")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read repo schema: %v", err)
	}
	return string(data)
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type sprintSeed struct {
	SprintID             string
	SequenceNo           int
	GitHubIssueNumber    int
	Status               string
	SprintBranch         *string
	ActiveSprintPRNumber *int
	NeedsHuman           bool
	HumanReason          *string
}

func insertSprintRow(t *testing.T, service *store.Service, seed sprintSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	createdAt := "2026-03-07T00:00:00Z"
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO sprints (
			sprint_id,
			sequence_no,
			github_issue_number,
			github_issue_node_id,
			title,
			body_md,
			goal,
			done_when_json,
			status,
			sprint_branch,
			active_sprint_pr_number,
			timeline_log_path,
			needs_human,
			human_reason,
			opened_at,
			closed_at,
			last_issue_sync_at,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.SprintID,
		seed.SequenceNo,
		seed.GitHubIssueNumber,
		seed.SprintID+"-node",
		seed.SprintID+" title",
		"body",
		"goal",
		`["done"]`,
		seed.Status,
		seed.SprintBranch,
		seed.ActiveSprintPRNumber,
		"logs/"+seed.SprintID+".log",
		boolToInt(seed.NeedsHuman),
		seed.HumanReason,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert sprint row: %v", err)
	}
}

type taskSeed struct {
	TaskID                  string
	SprintID                string
	TaskLocalID             string
	SequenceNo              int
	GitHubIssueNumber       int
	ParentGitHubIssueNumber int
	Status                  string
	ActivePRNumber          *int
	TaskBranch              *string
	WorktreePath            *string
	NeedsHuman              bool
	HumanReason             *string
}

func insertTaskRow(t *testing.T, service *store.Service, seed taskSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	insertTaskRowWithDB(t, db, seed)
}

func insertCorruptTaskRow(t *testing.T, dbPath string, seed taskSeed) {
	t.Helper()

	db, err := sql.Open(sqliteshim.ShimName, dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	insertTaskRowWithDB(t, db, seed)
}

func insertTaskRowWithDB(t *testing.T, db execContext, seed taskSeed) {
	t.Helper()

	createdAt := "2026-03-07T00:00:00Z"
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO tasks (
			task_id,
			sprint_id,
			task_local_id,
			sequence_no,
			github_issue_number,
			github_issue_node_id,
			parent_github_issue_number,
			title,
			body_md,
			goal,
			acceptance_criteria_json,
			out_of_scope_json,
			status,
			attempt_total,
			qa_fail_count,
			review_fail_count,
			ci_fail_count,
			current_failure_fingerprint,
			active_pr_number,
			task_branch,
			worktree_path,
			needs_human,
			human_reason,
			opened_at,
			closed_at,
			last_issue_sync_at,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.TaskID,
		seed.SprintID,
		seed.TaskLocalID,
		seed.SequenceNo,
		seed.GitHubIssueNumber,
		seed.TaskID+"-node",
		seed.ParentGitHubIssueNumber,
		seed.TaskID+" title",
		"body",
		"goal",
		`["ship it"]`,
		`["none"]`,
		seed.Status,
		0,
		0,
		0,
		0,
		nil,
		seed.ActivePRNumber,
		seed.TaskBranch,
		seed.WorktreePath,
		boolToInt(seed.NeedsHuman),
		seed.HumanReason,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert task row: %v", err)
	}
}

func insertDependencyRow(t *testing.T, service *store.Service, taskID, dependsOnTaskID string) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO task_dependencies (task_id, depends_on_task_id, source, created_at) VALUES (?, ?, ?, ?)`,
		taskID,
		dependsOnTaskID,
		"github_issue_dependency",
		"2026-03-07T00:00:00Z",
	); err != nil {
		t.Fatalf("insert dependency row: %v", err)
	}
}

func insertCorruptDependencyRow(t *testing.T, dbPath, taskID, dependsOnTaskID string) {
	t.Helper()

	db, err := sql.Open(sqliteshim.ShimName, dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign keys: %v", err)
	}
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO task_dependencies (task_id, depends_on_task_id, source, created_at) VALUES (?, ?, ?, ?)`,
		taskID,
		dependsOnTaskID,
		"github_issue_dependency",
		"2026-03-07T00:00:00Z",
	); err != nil {
		t.Fatalf("insert corrupt dependency row: %v", err)
	}
}

type execContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func sprintIDs(values []store.SprintProjection) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.SprintID)
	}
	return result
}

func taskIDs(values []TaskEntry) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Task.TaskID)
	}
	return result
}

func assertContainsReason(t *testing.T, values []string, expected string) {
	t.Helper()

	for _, value := range values {
		if value == expected {
			return
		}
	}
	t.Fatalf("expected reason %q in %v", expected, values)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
