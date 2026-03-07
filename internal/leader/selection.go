package leader

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
)

type SelectionStatus string

const (
	SelectionStatusSelected            SelectionStatus = "selected"
	SelectionStatusNoSchedulableSprint SelectionStatus = "no_schedulable_sprint"
	SelectionStatusNoSchedulableTask   SelectionStatus = "no_schedulable_task"
)

type SprintSelectionResult struct {
	Status         SelectionStatus          `json:"status"`
	Sprint         *store.SprintProjection  `json:"sprint,omitempty"`
	Reason         string                   `json:"reason,omitempty"`
	BlockingIssues []tasklist.BlockingIssue `json:"blocking_issues"`
}

type TaskSelectionResult struct {
	Status         SelectionStatus          `json:"status"`
	SprintID       string                   `json:"sprint_id"`
	Task           *store.TaskProjection    `json:"task,omitempty"`
	Reason         string                   `json:"reason,omitempty"`
	BlockingIssues []tasklist.BlockingIssue `json:"blocking_issues"`
}

type WorkItemSelectionResult struct {
	Status         SelectionStatus          `json:"status"`
	Sprint         *store.SprintProjection  `json:"sprint,omitempty"`
	Task           *store.TaskProjection    `json:"task,omitempty"`
	Reason         string                   `json:"reason,omitempty"`
	BlockingIssues []tasklist.BlockingIssue `json:"blocking_issues"`
}

func (s *Service) SelectNextSprint(ctx context.Context) (SprintSelectionResult, error) {
	response, err := s.loadTaskList(ctx, tasklist.Request{
		RefreshMode: tasklist.RefreshModeFull,
	})
	if err != nil {
		return SprintSelectionResult{}, err
	}

	return selectNextSprint(response), nil
}

func (s *Service) SelectNextTask(ctx context.Context, sprintID string) (TaskSelectionResult, error) {
	sprintID = strings.TrimSpace(sprintID)
	if sprintID == "" {
		return TaskSelectionResult{}, errors.New("sprint_id is required")
	}

	response, err := s.loadTaskList(ctx, tasklist.Request{
		RefreshMode: tasklist.RefreshModeTargeted,
		SprintID:    sprintID,
	})
	if err != nil {
		return TaskSelectionResult{}, err
	}

	return selectNextTask(sprintID, response), nil
}

func (s *Service) SelectNextWorkItem(ctx context.Context) (WorkItemSelectionResult, error) {
	sprintResult, err := s.SelectNextSprint(ctx)
	if err != nil {
		return WorkItemSelectionResult{}, err
	}
	if sprintResult.Status != SelectionStatusSelected || sprintResult.Sprint == nil {
		return WorkItemSelectionResult{
			Status:         SelectionStatusNoSchedulableSprint,
			Reason:         sprintResult.Reason,
			BlockingIssues: copyBlockingIssues(sprintResult.BlockingIssues),
		}, nil
	}

	taskResult, err := s.SelectNextTask(ctx, sprintResult.Sprint.SprintID)
	if err != nil {
		return WorkItemSelectionResult{}, err
	}
	if taskResult.Status != SelectionStatusSelected || taskResult.Task == nil {
		return WorkItemSelectionResult{
			Status:         SelectionStatusNoSchedulableTask,
			Sprint:         copySprintProjection(sprintResult.Sprint),
			Reason:         taskResult.Reason,
			BlockingIssues: copyBlockingIssues(taskResult.BlockingIssues),
		}, nil
	}

	return WorkItemSelectionResult{
		Status:         SelectionStatusSelected,
		Sprint:         copySprintProjection(sprintResult.Sprint),
		Task:           copyTaskProjection(taskResult.Task),
		BlockingIssues: []tasklist.BlockingIssue{},
	}, nil
}

func (s *Service) loadTaskList(ctx context.Context, req tasklist.Request) (tasklist.ResponseData, error) {
	if ctx == nil {
		return tasklist.ResponseData{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return tasklist.ResponseData{}, err
	}
	if s.tasklist == nil {
		return tasklist.ResponseData{}, errors.New("task list tool is required")
	}

	response := s.tasklist.Execute(ctx, req)
	if !response.OK {
		if response.Error != nil {
			return tasklist.ResponseData{}, fmt.Errorf("load task list: %w", response.Error)
		}
		return tasklist.ResponseData{}, errors.New("load task list: unknown failure")
	}
	if response.Data == nil {
		return tasklist.ResponseData{}, errors.New("load task list: missing response data")
	}
	return *response.Data, nil
}

func selectNextSprint(data tasklist.ResponseData) SprintSelectionResult {
	sprints := sortedSprints(data.Sprints)
	for _, sprint := range sprints {
		if !isSprintSchedulable(sprint.Status) {
			continue
		}

		selected := sprint
		return SprintSelectionResult{
			Status:         SelectionStatusSelected,
			Sprint:         &selected,
			BlockingIssues: []tasklist.BlockingIssue{},
		}
	}

	reason := "no projected sprints available"
	if len(sprints) > 0 {
		reason = "no projected sprint is currently startable"
	}

	return SprintSelectionResult{
		Status:         SelectionStatusNoSchedulableSprint,
		Reason:         reason,
		BlockingIssues: copyBlockingIssues(data.BlockingIssues),
	}
}

func selectNextTask(sprintID string, data tasklist.ResponseData) TaskSelectionResult {
	sprintID = strings.TrimSpace(sprintID)
	entries := sortedTaskEntriesForSprint(data.Tasks, sprintID)
	if len(entries) == 0 {
		return TaskSelectionResult{
			Status:         SelectionStatusNoSchedulableTask,
			SprintID:       sprintID,
			Reason:         fmt.Sprintf("sprint %s has no projected tasks", sprintID),
			BlockingIssues: copyBlockingIssues(data.BlockingIssues),
		}
	}

	for _, entry := range entries {
		if len(normalizedBlockedReasons(entry.BlockedBy)) != 0 {
			continue
		}

		selected := entry.Task
		return TaskSelectionResult{
			Status:         SelectionStatusSelected,
			SprintID:       sprintID,
			Task:           &selected,
			BlockingIssues: []tasklist.BlockingIssue{},
		}
	}

	return TaskSelectionResult{
		Status:         SelectionStatusNoSchedulableTask,
		SprintID:       sprintID,
		Reason:         fmt.Sprintf("no schedulable task found in sprint %s", sprintID),
		BlockingIssues: copyBlockingIssues(data.BlockingIssues),
	}
}

func sortedSprints(values []store.SprintProjection) []store.SprintProjection {
	if len(values) == 0 {
		return nil
	}

	result := make([]store.SprintProjection, len(values))
	copy(result, values)
	sort.Slice(result, func(i, j int) bool {
		if result[i].SequenceNo != result[j].SequenceNo {
			return result[i].SequenceNo < result[j].SequenceNo
		}
		return result[i].SprintID < result[j].SprintID
	})
	return result
}

func sortedTaskEntriesForSprint(values []tasklist.TaskEntry, sprintID string) []tasklist.TaskEntry {
	if len(values) == 0 {
		return nil
	}

	result := make([]tasklist.TaskEntry, 0, len(values))
	for _, entry := range values {
		if strings.TrimSpace(entry.Task.SprintID) != sprintID {
			continue
		}
		copied := entry
		copied.BlockedBy = normalizedBlockedReasons(entry.BlockedBy)
		result = append(result, copied)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Task.SequenceNo != result[j].Task.SequenceNo {
			return result[i].Task.SequenceNo < result[j].Task.SequenceNo
		}
		if result[i].Task.TaskLocalID != result[j].Task.TaskLocalID {
			return result[i].Task.TaskLocalID < result[j].Task.TaskLocalID
		}
		return result[i].Task.TaskID < result[j].Task.TaskID
	})
	return result
}

func normalizedBlockedReasons(values []string) []string {
	if len(values) == 0 {
		return nil
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

func isSprintSchedulable(status string) bool {
	switch strings.TrimSpace(status) {
	case "todo", "in_progress", "partially_done":
		return true
	default:
		return false
	}
}

func copyBlockingIssues(values []tasklist.BlockingIssue) []tasklist.BlockingIssue {
	if len(values) == 0 {
		return []tasklist.BlockingIssue{}
	}

	result := make([]tasklist.BlockingIssue, len(values))
	copy(result, values)
	return result
}

func copySprintProjection(value *store.SprintProjection) *store.SprintProjection {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyTaskProjection(value *store.TaskProjection) *store.TaskProjection {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
