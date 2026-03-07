package leader

import (
	"context"
	"errors"
	"strings"

	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/worktreeprep"
)

type PrepareNextWorkItemOptions struct {
	WorkDir       string
	DefaultBranch string
	WorktreeRoot  string
	Remote        string
}

type PreparedWorkItemResult struct {
	Status         SelectionStatus            `json:"status"`
	Sprint         *store.SprintProjection    `json:"sprint,omitempty"`
	Task           *store.TaskProjection      `json:"task,omitempty"`
	Worktree       *worktreeprep.ResponseData `json:"worktree,omitempty"`
	Reason         string                     `json:"reason,omitempty"`
	BlockingIssues []tasklist.BlockingIssue   `json:"blocking_issues"`
}

func (s *Service) PrepareNextWorkItem(ctx context.Context, opts PrepareNextWorkItemOptions) (PreparedWorkItemResult, error) {
	selection, err := s.SelectNextWorkItem(ctx)
	if err != nil {
		return PreparedWorkItemResult{}, err
	}
	if selection.Status != SelectionStatusSelected || selection.Sprint == nil || selection.Task == nil {
		return preparedWorkItemResultFromSelection(selection), nil
	}

	unlock := s.lockTaskStartup(selection.Task.TaskID)
	defer unlock()
	return s.prepareSelectedWorkItem(ctx, selection, opts)
}

func preparedWorkItemResultFromSelection(selection WorkItemSelectionResult) PreparedWorkItemResult {
	return PreparedWorkItemResult{
		Status:         selection.Status,
		Sprint:         copySprintProjection(selection.Sprint),
		Task:           copyTaskProjection(selection.Task),
		Reason:         selection.Reason,
		BlockingIssues: copyBlockingIssues(selection.BlockingIssues),
	}
}

func (s *Service) prepareSelectedWorkItem(ctx context.Context, selection WorkItemSelectionResult, opts PrepareNextWorkItemOptions) (PreparedWorkItemResult, error) {
	result := preparedWorkItemResultFromSelection(selection)
	if s.worktreePrep == nil {
		return PreparedWorkItemResult{}, errors.New("prepare worktree tool is required")
	}

	response := s.worktreePrep.Execute(ctx, worktreeprep.Request{
		SprintID:     selection.Sprint.SprintID,
		TaskID:       selection.Task.TaskID,
		SprintBranch: effectiveSprintBranch(selection.Sprint),
		TaskBranch:   effectiveTaskBranch(selection.Task),
		WorktreeRoot: strings.TrimSpace(opts.WorktreeRoot),
	}, worktreeprep.ExecuteOptions{
		WorkDir:       strings.TrimSpace(opts.WorkDir),
		DefaultBranch: strings.TrimSpace(opts.DefaultBranch),
		Remote:        strings.TrimSpace(opts.Remote),
	})
	if !response.OK {
		if response.Error != nil {
			return PreparedWorkItemResult{}, response.Error
		}
		return PreparedWorkItemResult{}, errors.New("prepare worktree: unknown failure")
	}
	if response.Data == nil {
		return PreparedWorkItemResult{}, errors.New("prepare worktree: missing response data")
	}

	copied := *response.Data
	result.Worktree = &copied
	return result, nil
}

func effectiveSprintBranch(sprint *store.SprintProjection) string {
	if sprint != nil && sprint.SprintBranch != nil && strings.TrimSpace(*sprint.SprintBranch) != "" {
		return strings.TrimSpace(*sprint.SprintBranch)
	}
	if sprint == nil {
		return ""
	}
	return "sprint/" + sprint.SprintID
}

func effectiveTaskBranch(task *store.TaskProjection) string {
	if task != nil && task.TaskBranch != nil && strings.TrimSpace(*task.TaskBranch) != "" {
		return strings.TrimSpace(*task.TaskBranch)
	}
	if task == nil {
		return ""
	}
	return "task/" + task.TaskID
}
