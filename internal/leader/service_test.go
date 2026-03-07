package leader

import (
	"context"
	"testing"

	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/worktreeprep"
)

func TestSelectNextSprintChoosesEarliestNonTerminalSprint(t *testing.T) {
	tool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-03", SequenceNo: 3, Status: "todo"},
					{SprintID: "Sprint-01", SequenceNo: 1, Status: "done"},
					{SprintID: "Sprint-02", SequenceNo: 2, Status: "in_progress"},
				},
			}),
		},
	}

	service := New(Dependencies{TaskList: tool})

	result, err := service.SelectNextSprint(context.Background())
	if err != nil {
		t.Fatalf("select next sprint: %v", err)
	}
	if result.Status != SelectionStatusSelected {
		t.Fatalf("expected selected status, got %s", result.Status)
	}
	if result.Sprint == nil || result.Sprint.SprintID != "Sprint-02" {
		t.Fatalf("expected Sprint-02, got %+v", result.Sprint)
	}
	if len(tool.calls) != 1 || tool.calls[0].RefreshMode != tasklist.RefreshModeFull {
		t.Fatalf("unexpected tasklist calls: %+v", tool.calls)
	}
}

func TestSelectNextTaskChoosesEarliestUnblockedTaskWithinSprint(t *testing.T) {
	tool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{
				RefreshMode: tasklist.RefreshModeTargeted,
				SprintID:    "Sprint-02",
			}): successResponse(tasklist.ResponseData{
				Tasks: []tasklist.TaskEntry{
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-02/Task-03",
							SprintID:    "Sprint-02",
							TaskLocalID: "Task-03",
							SequenceNo:  3,
							Status:      "todo",
						},
						BlockedBy: []string{"waiting for prior task Sprint-02/Task-02 to finish"},
					},
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-02/Task-01",
							SprintID:    "Sprint-02",
							TaskLocalID: "Task-01",
							SequenceNo:  1,
							Status:      "done",
						},
						BlockedBy: []string{"task is already completed"},
					},
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-02/Task-02",
							SprintID:    "Sprint-02",
							TaskLocalID: "Task-02",
							SequenceNo:  2,
							Status:      "todo",
						},
					},
				},
			}),
		},
	}

	service := New(Dependencies{TaskList: tool})

	result, err := service.SelectNextTask(context.Background(), "Sprint-02")
	if err != nil {
		t.Fatalf("select next task: %v", err)
	}
	if result.Status != SelectionStatusSelected {
		t.Fatalf("expected selected status, got %s", result.Status)
	}
	if result.Task == nil || result.Task.TaskID != "Sprint-02/Task-02" {
		t.Fatalf("expected Sprint-02/Task-02, got %+v", result.Task)
	}
	if len(tool.calls) != 1 || tool.calls[0].SprintID != "Sprint-02" {
		t.Fatalf("unexpected tasklist calls: %+v", tool.calls)
	}
}

func TestSelectNextWorkItemReturnsNoSchedulableTaskForCurrentSprint(t *testing.T) {
	tool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-02", SequenceNo: 2, Status: "todo"},
					{SprintID: "Sprint-01", SequenceNo: 1, Status: "awaiting_human"},
				},
			}),
			responseKey(tasklist.Request{
				RefreshMode: tasklist.RefreshModeTargeted,
				SprintID:    "Sprint-01",
			}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-01", SequenceNo: 1, Status: "awaiting_human"},
				},
				Tasks: []tasklist.TaskEntry{
					{
						Task: store.TaskProjection{
							TaskID:      "Sprint-01/Task-01",
							SprintID:    "Sprint-01",
							TaskLocalID: "Task-01",
							SequenceNo:  1,
							Status:      "done",
						},
						BlockedBy: []string{"task is already completed"},
					},
				},
			}),
		},
	}

	service := New(Dependencies{TaskList: tool})

	result, err := service.SelectNextWorkItem(context.Background())
	if err != nil {
		t.Fatalf("select next work item: %v", err)
	}
	if result.Status != SelectionStatusNoSchedulableTask {
		t.Fatalf("expected no_schedulable_task, got %s", result.Status)
	}
	if result.Sprint == nil || result.Sprint.SprintID != "Sprint-01" {
		t.Fatalf("expected Sprint-01 to block scheduling, got %+v", result.Sprint)
	}
	if result.Task != nil {
		t.Fatalf("expected no task to be selected, got %+v", result.Task)
	}
	if result.Reason != "no schedulable task found in sprint Sprint-01" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
	if len(tool.calls) != 2 {
		t.Fatalf("expected 2 tasklist calls, got %+v", tool.calls)
	}
	if tool.calls[1].SprintID != "Sprint-01" {
		t.Fatalf("expected targeted call for Sprint-01, got %+v", tool.calls[1])
	}
}

func TestSelectNextWorkItemReturnsExplicitNoSprintWhenAllProjectedSprintsAreTerminal(t *testing.T) {
	tool := &fakeTaskListTool{
		responses: map[string]tasklist.Response{
			responseKey(tasklist.Request{RefreshMode: tasklist.RefreshModeFull}): successResponse(tasklist.ResponseData{
				Sprints: []store.SprintProjection{
					{SprintID: "Sprint-02", SequenceNo: 2, Status: "blocked"},
					{SprintID: "Sprint-01", SequenceNo: 1, Status: "done"},
				},
				BlockingIssues: []tasklist.BlockingIssue{
					{Scope: "sprint", EntityID: "Sprint-02", Reason: "sprint requires human intervention"},
				},
			}),
		},
	}

	service := New(Dependencies{TaskList: tool})

	result, err := service.SelectNextWorkItem(context.Background())
	if err != nil {
		t.Fatalf("select next work item: %v", err)
	}
	if result.Status != SelectionStatusNoSchedulableSprint {
		t.Fatalf("expected no_schedulable_sprint, got %s", result.Status)
	}
	if result.Sprint != nil || result.Task != nil {
		t.Fatalf("expected no sprint/task, got sprint=%+v task=%+v", result.Sprint, result.Task)
	}
	if result.Reason != "all projected sprints are in terminal states" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
	if len(result.BlockingIssues) != 1 || result.BlockingIssues[0].EntityID != "Sprint-02" {
		t.Fatalf("expected blocking issues to be propagated, got %+v", result.BlockingIssues)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("expected a single full tasklist call, got %+v", tool.calls)
	}
}

func TestPrepareNextWorkItemUsesSelectedSprintAndTaskBranches(t *testing.T) {
	tool := &fakeTaskListTool{
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
							TaskID:      "Sprint-03/Task-03",
							SprintID:    "Sprint-03",
							TaskLocalID: "Task-03",
							SequenceNo:  3,
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
				WorktreePath:  "/tmp/worktrees/Sprint-03/Task-03",
				TaskBranch:    "task/Sprint-03/Task-03",
				BaseBranch:    "sprint/Sprint-03",
				BaseCommitSHA: "abc123",
				Reused:        true,
			},
		},
	}

	service := New(Dependencies{
		TaskList:     tool,
		WorktreePrep: worktreeTool,
	})

	result, err := service.PrepareNextWorkItem(context.Background(), PrepareNextWorkItemOptions{
		WorkDir:       "/repo",
		DefaultBranch: "main",
		WorktreeRoot:  "/tmp/worktrees",
		Remote:        "origin",
	})
	if err != nil {
		t.Fatalf("prepare next work item: %v", err)
	}
	if result.Status != SelectionStatusSelected {
		t.Fatalf("expected selected status, got %s", result.Status)
	}
	if result.Sprint == nil || result.Sprint.SprintID != "Sprint-03" {
		t.Fatalf("unexpected sprint: %+v", result.Sprint)
	}
	if result.Task == nil || result.Task.TaskID != "Sprint-03/Task-03" {
		t.Fatalf("unexpected task: %+v", result.Task)
	}
	if result.Worktree == nil || result.Worktree.BaseCommitSHA != "abc123" {
		t.Fatalf("unexpected worktree payload: %+v", result.Worktree)
	}
	if len(worktreeTool.calls) != 1 {
		t.Fatalf("expected a single worktree tool call, got %+v", worktreeTool.calls)
	}

	call := worktreeTool.calls[0]
	if call.request.SprintBranch != "sprint/Sprint-03" {
		t.Fatalf("unexpected sprint branch: %s", call.request.SprintBranch)
	}
	if call.request.TaskBranch != "task/Sprint-03/Task-03" {
		t.Fatalf("unexpected task branch: %s", call.request.TaskBranch)
	}
	if call.request.WorktreeRoot != "/tmp/worktrees" {
		t.Fatalf("unexpected worktree root: %s", call.request.WorktreeRoot)
	}
	if call.options.WorkDir != "/repo" || call.options.DefaultBranch != "main" || call.options.Remote != "origin" {
		t.Fatalf("unexpected execute options: %+v", call.options)
	}
}

type fakeTaskListTool struct {
	responses map[string]tasklist.Response
	calls     []tasklist.Request
}

type fakeWorktreePrepTool struct {
	response worktreeprep.Response
	calls    []fakeWorktreePrepCall
}

type fakeWorktreePrepCall struct {
	request worktreeprep.Request
	options worktreeprep.ExecuteOptions
}

func (f *fakeTaskListTool) Execute(_ context.Context, req tasklist.Request) tasklist.Response {
	f.calls = append(f.calls, req)

	response, ok := f.responses[responseKey(req)]
	if !ok {
		return tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:    tasklist.ErrorCodeInternal,
				Message: "unexpected request",
			},
		}
	}
	return response
}

func (f *fakeWorktreePrepTool) Execute(_ context.Context, req worktreeprep.Request, opts worktreeprep.ExecuteOptions) worktreeprep.Response {
	f.calls = append(f.calls, fakeWorktreePrepCall{
		request: req,
		options: opts,
	})
	return f.response
}

func successResponse(data tasklist.ResponseData) tasklist.Response {
	return tasklist.Response{
		OK:   true,
		Data: &data,
	}
}

func responseKey(req tasklist.Request) string {
	return string(req.RefreshMode) + "|" + req.SprintID
}
