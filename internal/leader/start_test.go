package leader

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/worktreeprep"
)

func TestStartPreparedWorkItemWritesEventsAndPersistsRecoveryRefs(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "todo",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "todo",
	})

	service := New(Dependencies{Store: storeService})
	worktree := newLeaderWorktreeData(t, "Sprint-03", "Sprint-03/Task-04")

	result, err := service.StartPreparedWorkItem(context.Background(), PreparedWorkItemResult{
		Status:   SelectionStatusSelected,
		Sprint:   &store.SprintProjection{SprintID: "Sprint-03"},
		Task:     &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
		Worktree: worktree,
	})
	if err != nil {
		t.Fatalf("start prepared work item: %v", err)
	}
	if result.Attempt != 1 {
		t.Fatalf("expected attempt 1, got %d", result.Attempt)
	}
	if result.Resumed {
		t.Fatal("expected first start not to be marked as resumed")
	}
	if result.Sprint == nil || result.Sprint.Status != "in_progress" {
		t.Fatalf("unexpected sprint projection: %+v", result.Sprint)
	}
	if result.Task == nil || result.Task.Status != "in_progress" {
		t.Fatalf("unexpected task projection: %+v", result.Task)
	}
	if result.Events.TaskSelected == nil || result.Events.TaskSelected.Deduplicated {
		t.Fatalf("expected task_selected event to be appended, got %+v", result.Events.TaskSelected)
	}
	if result.Events.SprintInitialized == nil || result.Events.SprintInitialized.Deduplicated {
		t.Fatalf("expected sprint_initialized event to be appended, got %+v", result.Events.SprintInitialized)
	}
	if result.Events.TaskStarted == nil || result.Events.TaskStarted.Deduplicated {
		t.Fatalf("expected task_started event to be appended, got %+v", result.Events.TaskStarted)
	}

	assertLeaderEventTypes(t, storeService, []string{
		eventTaskSelected,
		eventSprintInitialized,
		eventTaskStarted,
	})

	sprint := loadLeaderSprintRow(t, storeService, "Sprint-03")
	if sprint.Status != "in_progress" {
		t.Fatalf("unexpected sprint status: %s", sprint.Status)
	}
	if sprint.SprintBranch == nil || *sprint.SprintBranch != "sprint/Sprint-03" {
		t.Fatalf("unexpected sprint branch: %#v", sprint.SprintBranch)
	}

	task := loadLeaderTaskRow(t, storeService, "Sprint-03/Task-04")
	if task.Status != "in_progress" {
		t.Fatalf("unexpected task status: %s", task.Status)
	}
	if task.AttemptTotal != 1 {
		t.Fatalf("unexpected attempt_total: %d", task.AttemptTotal)
	}
	if task.TaskBranch == nil || *task.TaskBranch != "task/Sprint-03/Task-04" {
		t.Fatalf("unexpected task branch: %#v", task.TaskBranch)
	}
	if task.WorktreePath == nil || *task.WorktreePath != worktree.WorktreePath {
		t.Fatalf("unexpected worktree path: %#v", task.WorktreePath)
	}
}

func TestStartPreparedWorkItemIsIdempotentForRepeatedStartup(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "todo",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "todo",
	})

	prepared := PreparedWorkItemResult{
		Status:   SelectionStatusSelected,
		Sprint:   &store.SprintProjection{SprintID: "Sprint-03"},
		Task:     &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
		Worktree: newLeaderWorktreeData(t, "Sprint-03", "Sprint-03/Task-04"),
	}
	service := New(Dependencies{Store: storeService})

	if _, err := service.StartPreparedWorkItem(context.Background(), prepared); err != nil {
		t.Fatalf("first start prepared work item: %v", err)
	}
	firstSprint := loadLeaderSprintRow(t, storeService, "Sprint-03")
	firstTask := loadLeaderTaskRow(t, storeService, "Sprint-03/Task-04")
	time.Sleep(1100 * time.Millisecond)

	second, err := service.StartPreparedWorkItem(context.Background(), prepared)
	if err != nil {
		t.Fatalf("second start prepared work item: %v", err)
	}
	if second.Attempt != 1 {
		t.Fatalf("expected stable attempt 1, got %d", second.Attempt)
	}
	if !second.Resumed {
		t.Fatal("expected repeated startup to be marked as resumed")
	}
	if second.Events.TaskSelected == nil || !second.Events.TaskSelected.Deduplicated {
		t.Fatalf("expected task_selected dedupe, got %+v", second.Events.TaskSelected)
	}
	if second.Events.SprintInitialized != nil {
		t.Fatalf("did not expect sprint_initialized replay after sprint left todo, got %+v", second.Events.SprintInitialized)
	}
	if second.Events.TaskStarted == nil || !second.Events.TaskStarted.Deduplicated {
		t.Fatalf("expected task_started dedupe, got %+v", second.Events.TaskStarted)
	}

	assertLeaderEventTypes(t, storeService, []string{
		eventTaskSelected,
		eventSprintInitialized,
		eventTaskStarted,
	})

	task := loadLeaderTaskRow(t, storeService, "Sprint-03/Task-04")
	if task.AttemptTotal != 1 {
		t.Fatalf("expected attempt_total to remain 1, got %d", task.AttemptTotal)
	}
	if task.UpdatedAt != firstTask.UpdatedAt {
		t.Fatalf("expected repeated startup not to refresh task updated_at: got %s want %s", task.UpdatedAt, firstTask.UpdatedAt)
	}
	sprint := loadLeaderSprintRow(t, storeService, "Sprint-03")
	if sprint.UpdatedAt != firstSprint.UpdatedAt {
		t.Fatalf("expected repeated startup not to refresh sprint updated_at: got %s want %s", sprint.UpdatedAt, firstSprint.UpdatedAt)
	}
}

func TestStartPreparedWorkItemKeepsPartiallyDoneSprintStatus(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "partially_done",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "todo",
	})

	service := New(Dependencies{Store: storeService})

	result, err := service.StartPreparedWorkItem(context.Background(), PreparedWorkItemResult{
		Status:   SelectionStatusSelected,
		Sprint:   &store.SprintProjection{SprintID: "Sprint-03"},
		Task:     &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
		Worktree: newLeaderWorktreeData(t, "Sprint-03", "Sprint-03/Task-04"),
	})
	if err != nil {
		t.Fatalf("start prepared work item: %v", err)
	}
	if result.Sprint == nil || result.Sprint.Status != "partially_done" {
		t.Fatalf("expected sprint to stay partially_done, got %+v", result.Sprint)
	}
	if result.Events.SprintInitialized != nil {
		t.Fatalf("did not expect sprint_initialized event, got %+v", result.Events.SprintInitialized)
	}

	assertLeaderEventTypes(t, storeService, []string{
		eventTaskSelected,
		eventTaskStarted,
	})
}

func TestStartPreparedWorkItemRejectsInvalidPreparedInput(t *testing.T) {
	t.Parallel()

	worktree := newLeaderWorktreeData(t, "Sprint-03", "Sprint-03/Task-04")

	tests := []struct {
		name     string
		prepared PreparedWorkItemResult
		wantErr  string
	}{
		{
			name: "status must be selected",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusNoSchedulableTask,
			},
			wantErr: "prepared work item status must be selected",
		},
		{
			name: "missing sprint",
			prepared: PreparedWorkItemResult{
				Status:   SelectionStatusSelected,
				Task:     &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: worktree,
			},
			wantErr: "prepared sprint is required",
		},
		{
			name: "missing task",
			prepared: PreparedWorkItemResult{
				Status:   SelectionStatusSelected,
				Sprint:   &store.SprintProjection{SprintID: "Sprint-03"},
				Worktree: worktree,
			},
			wantErr: "prepared task is required",
		},
		{
			name: "missing worktree",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
			},
			wantErr: "prepared worktree data is required",
		},
		{
			name: "missing worktree path",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					TaskBranch:    "task/Sprint-03/Task-04",
					BaseBranch:    "sprint/Sprint-03",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared worktree path is required",
		},
		{
			name: "missing task branch",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  t.TempDir(),
					BaseBranch:    "sprint/Sprint-03",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared task branch is required",
		},
		{
			name: "missing sprint branch",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  t.TempDir(),
					TaskBranch:    "task/Sprint-03/Task-04",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared sprint branch is required",
		},
		{
			name: "missing base commit sha",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath: t.TempDir(),
					TaskBranch:   "task/Sprint-03/Task-04",
					BaseBranch:   "sprint/Sprint-03",
				},
			},
			wantErr: "prepared base commit sha is required",
		},
		{
			name: "task branch must match task id",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  t.TempDir(),
					TaskBranch:    "task/Sprint-03/Task-99",
					BaseBranch:    "sprint/Sprint-03",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared task branch must be task/Sprint-03/Task-04",
		},
		{
			name: "sprint branch must match sprint id",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  t.TempDir(),
					TaskBranch:    "task/Sprint-03/Task-04",
					BaseBranch:    "sprint/Sprint-99",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared sprint branch must be sprint/Sprint-03",
		},
		{
			name: "worktree path must be absolute",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  "relative/path",
					TaskBranch:    "task/Sprint-03/Task-04",
					BaseBranch:    "sprint/Sprint-03",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared worktree path must be absolute",
		},
		{
			name: "worktree path must exist",
			prepared: PreparedWorkItemResult{
				Status: SelectionStatusSelected,
				Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
				Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
				Worktree: &worktreeprep.ResponseData{
					WorktreePath:  filepath.Join(t.TempDir(), "missing"),
					TaskBranch:    "task/Sprint-03/Task-04",
					BaseBranch:    "sprint/Sprint-03",
					BaseCommitSHA: "abc123",
				},
			},
			wantErr: "prepared worktree path",
		},
	}

	service := New(Dependencies{})
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.StartPreparedWorkItem(context.Background(), tc.prepared)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStartNextWorkItemReturnsNoSchedulableResultWithoutStartupError(t *testing.T) {
	t.Parallel()

	taskListTool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-03", SequenceNo: 3, Status: "done"},
				},
			}),
		},
	}

	service := New(Dependencies{TaskList: taskListTool})
	result, err := service.StartNextWorkItem(context.Background(), PrepareNextWorkItemOptions{})
	if err != nil {
		t.Fatalf("start next work item: %v", err)
	}
	if result.Status != SelectionStatusNoSchedulableSprint {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.Reason != "no projected sprint is currently startable" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
	if result.Sprint != nil || result.Task != nil || result.Worktree != nil {
		t.Fatalf("expected empty selection result, got %+v", result)
	}
}

func TestStartPreparedWorkItemAllowsRecoveringToNewWorktreePathWhenStoredPathIsGone(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	missingPath := filepath.Join(t.TempDir(), "missing-worktree")
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "in_progress",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPointer("task/Sprint-03/Task-04"),
		WorktreePath:            &missingPath,
	})

	service := New(Dependencies{Store: storeService})
	result, err := service.StartPreparedWorkItem(context.Background(), PreparedWorkItemResult{
		Status: SelectionStatusSelected,
		Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
		Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
		Worktree: &worktreeprep.ResponseData{
			WorktreePath:  t.TempDir(),
			TaskBranch:    "task/Sprint-03/Task-04",
			BaseBranch:    "sprint/Sprint-03",
			BaseCommitSHA: "abc123",
			Reused:        true,
		},
	})
	if err != nil {
		t.Fatalf("start prepared work item: %v", err)
	}
	if result.Task == nil || result.Task.WorktreePath == nil || *result.Task.WorktreePath != preparedWorktreePath(result.Worktree) {
		t.Fatalf("expected relocated worktree path in result, got %+v", result.Task)
	}

	task := loadLeaderTaskRow(t, storeService, "Sprint-03/Task-04")
	if task.WorktreePath == nil || *task.WorktreePath != preparedWorktreePath(result.Worktree) {
		t.Fatalf("expected relocated worktree path in store, got %#v", task.WorktreePath)
	}
}

func TestStartPreparedWorkItemRejectsReplacingAccessibleWorktreePath(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	existingPath := t.TempDir()
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "in_progress",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "in_progress",
		AttemptTotal:            1,
		TaskBranch:              stringPointer("task/Sprint-03/Task-04"),
		WorktreePath:            &existingPath,
	})

	service := New(Dependencies{Store: storeService})
	_, err := service.StartPreparedWorkItem(context.Background(), PreparedWorkItemResult{
		Status: SelectionStatusSelected,
		Sprint: &store.SprintProjection{SprintID: "Sprint-03"},
		Task:   &store.TaskProjection{TaskID: "Sprint-03/Task-04", SprintID: "Sprint-03"},
		Worktree: &worktreeprep.ResponseData{
			WorktreePath:  t.TempDir(),
			TaskBranch:    "task/Sprint-03/Task-04",
			BaseBranch:    "sprint/Sprint-03",
			BaseCommitSHA: "abc123",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "worktree_path is already set") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartNextWorkItemComposesPrepareAndStartup(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "todo",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "todo",
	})

	taskListTool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-03", SequenceNo: 3, Status: "todo"},
				},
			}),
			responseKey(tasklist.Request{
				RefreshMode: tasklist.RefreshModeTargeted,
				SprintID:    "Sprint-03",
			}): successResponse(tasklist.ResponseData{
				Tasks: []tasklist.TaskEntry{
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-03/Task-04",
							SprintID:    "Sprint-03",
							TaskLocalID: "Task-04",
							SequenceNo:  4,
							Status:      "todo",
						},
					},
				},
			}),
		},
	}
	worktreeTool := &fakeWorktreePrepTool{
		response: worktreeprep.Response{
			OK: true,
			Data: &worktreeprep.ResponseData{
				WorktreePath:  t.TempDir(),
				TaskBranch:    "task/Sprint-03/Task-04",
				BaseBranch:    "sprint/Sprint-03",
				BaseCommitSHA: "abc123",
				Reused:        true,
			},
		},
	}

	service := New(Dependencies{
		Store:        storeService,
		TaskList:     taskListTool,
		WorktreePrep: worktreeTool,
	})

	result, err := service.StartNextWorkItem(context.Background(), PrepareNextWorkItemOptions{
		WorkDir:       "/repo",
		DefaultBranch: "main",
		WorktreeRoot:  "/tmp/worktrees",
		Remote:        "origin",
	})
	if err != nil {
		t.Fatalf("start next work item: %v", err)
	}
	if result.Status != SelectionStatusSelected {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.Worktree == nil || result.Worktree.WorktreePath == "" {
		t.Fatalf("unexpected worktree data: %+v", result.Worktree)
	}
	if result.Task == nil || result.Task.Status != "in_progress" {
		t.Fatalf("unexpected task projection: %+v", result.Task)
	}
	if result.Sprint == nil || result.Sprint.Status != "in_progress" {
		t.Fatalf("unexpected sprint projection: %+v", result.Sprint)
	}
	if len(worktreeTool.calls) != 1 {
		t.Fatalf("expected one worktree call, got %+v", worktreeTool.calls)
	}

	assertLeaderEventTypes(t, storeService, []string{
		eventTaskSelected,
		eventSprintInitialized,
		eventTaskStarted,
	})
}

func TestStartNextWorkItemSerializesPreparePhaseForSameTask(t *testing.T) {
	t.Parallel()

	storeService := openLeaderTestStore(t)
	insertLeaderSprintRow(t, storeService, leaderSprintSeed{
		SprintID:          "Sprint-03",
		SequenceNo:        3,
		GitHubIssueNumber: 301,
		Status:            "todo",
	})
	insertLeaderTaskRow(t, storeService, leaderTaskSeed{
		TaskID:                  "Sprint-03/Task-04",
		SprintID:                "Sprint-03",
		TaskLocalID:             "Task-04",
		SequenceNo:              4,
		GitHubIssueNumber:       304,
		ParentGitHubIssueNumber: 301,
		Status:                  "todo",
	})

	taskListTool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-03", SequenceNo: 3, Status: "todo"},
				},
			}),
			responseKey(tasklist.Request{
				RefreshMode: tasklist.RefreshModeTargeted,
				SprintID:    "Sprint-03",
			}): successResponse(tasklist.ResponseData{
				Tasks: []tasklist.TaskEntry{
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-03/Task-04",
							SprintID:    "Sprint-03",
							TaskLocalID: "Task-04",
							SequenceNo:  4,
							Status:      "todo",
						},
					},
				},
			}),
		},
	}
	worktreeTool := &observingWorktreePrepTool{
		response: worktreeprep.Response{
			OK:   true,
			Data: newLeaderWorktreeData(t, "Sprint-03", "Sprint-03/Task-04"),
		},
	}

	service := New(Dependencies{
		Store:        storeService,
		TaskList:     taskListTool,
		WorktreePrep: worktreeTool,
	})

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.StartNextWorkItem(context.Background(), PrepareNextWorkItemOptions{
				WorkDir:       "/repo",
				DefaultBranch: "main",
				WorktreeRoot:  "/tmp/worktrees",
				Remote:        "origin",
			})
			errs <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent start next work item: %v", err)
		}
	}
	if worktreeTool.maxActiveCalls() > 1 {
		t.Fatalf("expected worktree prep to be serialized, got max concurrency %d", worktreeTool.maxActiveCalls())
	}

	assertLeaderEventTypes(t, storeService, []string{
		eventTaskSelected,
		eventSprintInitialized,
		eventTaskStarted,
	})
}

type leaderSprintSeed struct {
	SprintID          string
	SequenceNo        int
	GitHubIssueNumber int
	Status            string
	SprintBranch      *string
}

type leaderTaskSeed struct {
	TaskID                  string
	SprintID                string
	TaskLocalID             string
	SequenceNo              int
	GitHubIssueNumber       int
	ParentGitHubIssueNumber int
	Status                  string
	AttemptTotal            int
	TaskBranch              *string
	WorktreePath            *string
}

type leaderSprintRow struct {
	Status       string
	SprintBranch *string
	UpdatedAt    string
}

type leaderTaskRow struct {
	Status       string
	AttemptTotal int
	TaskBranch   *string
	WorktreePath *string
	UpdatedAt    string
}

type observingWorktreePrepTool struct {
	mu        sync.Mutex
	active    int
	maxActive int
	response  worktreeprep.Response
}

func (o *observingWorktreePrepTool) Execute(_ context.Context, _ worktreeprep.Request, _ worktreeprep.ExecuteOptions) worktreeprep.Response {
	o.mu.Lock()
	o.active++
	if o.active > o.maxActive {
		o.maxActive = o.active
	}
	o.mu.Unlock()

	time.Sleep(25 * time.Millisecond)

	o.mu.Lock()
	o.active--
	o.mu.Unlock()
	return o.response
}

func (o *observingWorktreePrepTool) maxActiveCalls() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.maxActive
}

func openLeaderTestStore(t *testing.T) *store.Service {
	t.Helper()

	repoRoot := newLeaderTestRepoRoot(t)
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
	return service
}

func newLeaderTestRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeLeaderTestFile(t, root, filepath.Join("config", "config.yaml"), "store: test\n")
	writeLeaderTestFile(t, root, filepath.Join("sql", "schema.sql"), loadLeaderRepoSchema(t))
	return root
}

func loadLeaderRepoSchema(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "sql", "schema.sql"))
	if err != nil {
		t.Fatalf("read repo schema: %v", err)
	}
	return string(data)
}

func writeLeaderTestFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newLeaderWorktreeData(t *testing.T, sprintID, taskID string) *worktreeprep.ResponseData {
	t.Helper()

	return &worktreeprep.ResponseData{
		WorktreePath:  t.TempDir(),
		TaskBranch:    "task/" + taskID,
		BaseBranch:    "sprint/" + sprintID,
		BaseCommitSHA: "abc123",
	}
}

func preparedWorktreePath(value *worktreeprep.ResponseData) string {
	if value == nil {
		return ""
	}
	return value.WorktreePath
}

func insertLeaderSprintRow(t *testing.T, service *store.Service, seed leaderSprintSeed) {
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
		nil,
		"logs/"+seed.SprintID+".log",
		0,
		nil,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert sprint row: %v", err)
	}
}

func insertLeaderTaskRow(t *testing.T, service *store.Service, seed leaderTaskSeed) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

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
		seed.AttemptTotal,
		0,
		0,
		0,
		nil,
		nil,
		seed.TaskBranch,
		seed.WorktreePath,
		0,
		nil,
		nil,
		nil,
		nil,
		createdAt,
		createdAt,
	); err != nil {
		t.Fatalf("insert task row: %v", err)
	}
}

func assertLeaderEventTypes(t *testing.T, service *store.Service, want []string) {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), `SELECT event_type FROM events ORDER BY rowid ASC`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan event type: %v", err)
		}
		got = append(got, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event rows: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected event count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected event order: got %v want %v", got, want)
		}
	}
}

func loadLeaderSprintRow(t *testing.T, service *store.Service, sprintID string) leaderSprintRow {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var row leaderSprintRow
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT status, sprint_branch, updated_at FROM sprints WHERE sprint_id = ?`,
		sprintID,
	).Scan(&row.Status, &row.SprintBranch, &row.UpdatedAt); err != nil {
		t.Fatalf("load sprint row: %v", err)
	}
	return row
}

func loadLeaderTaskRow(t *testing.T, service *store.Service, taskID string) leaderTaskRow {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var row leaderTaskRow
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT status, attempt_total, task_branch, worktree_path, updated_at FROM tasks WHERE task_id = ?`,
		taskID,
	).Scan(&row.Status, &row.AttemptTotal, &row.TaskBranch, &row.WorktreePath, &row.UpdatedAt); err != nil {
		t.Fatalf("load task row: %v", err)
	}
	return row
}
