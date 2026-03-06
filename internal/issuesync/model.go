package issuesync

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	sprintHeadingPattern = regexp.MustCompile(`^## \[(Sprint-\d+)\] (.+)$`)
	taskHeadingPattern   = regexp.MustCompile(`^# \[(Sprint-\d+)\]\[(Task-\d+)\] (.+)$`)
	dependencyPattern    = regexp.MustCompile(`^(Sprint-\d+)/(Task-\d+)$`)
)

type PlanData struct {
	Sprints []*Sprint
	Tasks   map[string]*TaskBrief
}

type Sprint struct {
	ID        string
	Title     string
	Goal      string
	DoneWhen  []string
	TaskOrder []TaskSummary
	Source    string
}

type TaskSummary struct {
	TaskLocalID string
	Title       string
}

type TaskBrief struct {
	SprintID           string
	TaskLocalID        string
	TaskID             string
	Title              string
	Goal               string
	Reads              []string
	Dependencies       []string
	InScope            []string
	OutOfScope         []string
	Deliverables       []string
	AcceptanceCriteria []string
	Notes              []string
	Source             string
}

type DependencyRef struct {
	SprintID    string
	TaskLocalID string
	TaskID      string
}

func OrderedTasks(plan *PlanData) []*TaskBrief {
	var tasks []*TaskBrief
	for _, sprint := range plan.Sprints {
		for _, summary := range sprint.TaskOrder {
			taskID := fmt.Sprintf("%s/%s", sprint.ID, summary.TaskLocalID)
			if brief, ok := plan.Tasks[taskID]; ok {
				tasks = append(tasks, brief)
			}
		}
	}
	return tasks
}

func ParseDependencyRef(value string) (DependencyRef, bool) {
	normalized := strings.Trim(strings.TrimSpace(value), "`")
	match := dependencyPattern.FindStringSubmatch(normalized)
	if match == nil {
		return DependencyRef{}, false
	}

	return DependencyRef{
		SprintID:    match[1],
		TaskLocalID: match[2],
		TaskID:      fmt.Sprintf("%s/%s", match[1], match[2]),
	}, true
}
