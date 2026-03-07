package githubsync

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/store"
)

var (
	sprintTitlePattern         = regexp.MustCompile(`^\[(Sprint-\d+)\]\s+(.+)$`)
	taskTitlePattern           = regexp.MustCompile(`^\[(Sprint-\d+)\]\[(Task-\d+)\]\s+(.+)$`)
	sprintIDPattern            = regexp.MustCompile(`^Sprint-(\d+)$`)
	taskIDPattern              = regexp.MustCompile(`^Task-(\d+)$`)
	markdownHeadingPattern     = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	canonicalTaskBranchPattern = regexp.MustCompile(`^task/(Sprint-\d+)/(Task-\d+)$`)
	legacyTaskBranchPattern    = regexp.MustCompile(`^task/(Sprint-\d+)-(Task-\d+)$`)
	sprintBranchPattern        = regexp.MustCompile(`^sprint/(Sprint-\d+)$`)
)

type parsedSprint struct {
	Issue            toolgithub.Issue
	Snapshot         store.SprintDefinitionSnapshot
	TaskIssueNumbers []int
}

type parsedTask struct {
	Issue                  toolgithub.Issue
	Snapshot               store.TaskDefinitionSnapshot
	DependencyIssueNumbers []int
}

func parseSprintIssue(issue toolgithub.Issue) (parsedSprint, error) {
	if !issue.HasLabel("kind/sprint") {
		return parsedSprint{}, newValidationError("issue #%d is missing required label kind/sprint", issue.GitHubIssueNumber)
	}

	sprintID, title, err := parseSprintTitle(issue.Title)
	if err != nil {
		return parsedSprint{}, fmt.Errorf("parse sprint issue #%d title: %w", issue.GitHubIssueNumber, err)
	}

	sections := parseMarkdownSections(issue.Body)
	bodySprintID := strings.TrimSpace(sectionText(sections, "Sprint ID"))
	if bodySprintID == "" {
		return parsedSprint{}, newValidationError("sprint issue #%d is missing body section Sprint ID", issue.GitHubIssueNumber)
	}
	if bodySprintID != sprintID {
		return parsedSprint{}, newValidationError(
			"sprint issue #%d has mismatched sprint id: title=%s body=%s",
			issue.GitHubIssueNumber,
			sprintID,
			bodySprintID,
		)
	}

	sequenceNo, err := parseSprintSequence(sprintID)
	if err != nil {
		return parsedSprint{}, fmt.Errorf("parse sprint issue #%d sequence: %w", issue.GitHubIssueNumber, err)
	}

	return parsedSprint{
		Issue: issue,
		Snapshot: store.SprintDefinitionSnapshot{
			SprintID:          sprintID,
			SequenceNo:        sequenceNo,
			GitHubIssueNumber: issue.GitHubIssueNumber,
			GitHubIssueNodeID: issue.GitHubIssueNodeID,
			Title:             title,
			BodyMD:            issue.Body,
			Goal:              sectionText(sections, "Goal"),
			DoneWhen:          sectionList(sections, "Done When"),
			NeedsHuman:        issue.HasLabel("needs-human"),
			OpenedAt:          issue.CreatedAt,
			ClosedAt:          issue.ClosedAt,
			LastIssueSyncAt:   issue.UpdatedAt,
		},
	}, nil
}

func parseTaskIssue(issue toolgithub.Issue, parentSprintID string, parentIssueNumber int) (parsedTask, error) {
	if !issue.HasLabel("kind/task") {
		return parsedTask{}, newValidationError("issue #%d is missing required label kind/task", issue.GitHubIssueNumber)
	}

	sprintID, taskLocalID, title, err := parseTaskTitle(issue.Title)
	if err != nil {
		return parsedTask{}, fmt.Errorf("parse task issue #%d title: %w", issue.GitHubIssueNumber, err)
	}

	sections := parseMarkdownSections(issue.Body)
	bodySprintID := strings.TrimSpace(sectionText(sections, "Sprint ID"))
	if bodySprintID == "" {
		return parsedTask{}, newValidationError("task issue #%d is missing body section Sprint ID", issue.GitHubIssueNumber)
	}
	if bodySprintID != sprintID {
		return parsedTask{}, newValidationError(
			"task issue #%d has mismatched sprint id: title=%s body=%s",
			issue.GitHubIssueNumber,
			sprintID,
			bodySprintID,
		)
	}

	bodyTaskID := strings.TrimSpace(sectionText(sections, "Task ID"))
	if bodyTaskID == "" {
		return parsedTask{}, newValidationError("task issue #%d is missing body section Task ID", issue.GitHubIssueNumber)
	}
	if bodyTaskID != taskLocalID {
		return parsedTask{}, newValidationError(
			"task issue #%d has mismatched task id: title=%s body=%s",
			issue.GitHubIssueNumber,
			taskLocalID,
			bodyTaskID,
		)
	}

	if parentSprintID != "" && sprintID != parentSprintID {
		return parsedTask{}, newValidationError(
			"task issue #%d belongs to sprint %s but is linked under %s",
			issue.GitHubIssueNumber,
			sprintID,
			parentSprintID,
		)
	}

	sequenceNo, err := parseTaskSequence(taskLocalID)
	if err != nil {
		return parsedTask{}, fmt.Errorf("parse task issue #%d sequence: %w", issue.GitHubIssueNumber, err)
	}

	humanReason := ""
	if issue.HasLabel("needs-human") {
		humanReason = "needs-human label set on GitHub task issue"
	}

	return parsedTask{
		Issue: issue,
		Snapshot: store.TaskDefinitionSnapshot{
			TaskID:                  formatTaskID(sprintID, taskLocalID),
			SprintID:                sprintID,
			TaskLocalID:             taskLocalID,
			SequenceNo:              sequenceNo,
			GitHubIssueNumber:       issue.GitHubIssueNumber,
			GitHubIssueNodeID:       issue.GitHubIssueNodeID,
			ParentGitHubIssueNumber: parentIssueNumber,
			Title:                   title,
			BodyMD:                  issue.Body,
			Goal:                    sectionText(sections, "Goal"),
			AcceptanceCriteria:      sectionList(sections, "Acceptance Criteria"),
			OutOfScope:              sectionList(sections, "Out of Scope"),
			NeedsHuman:              issue.HasLabel("needs-human"),
			HumanReason:             optionalText(humanReason),
			OpenedAt:                issue.CreatedAt,
			ClosedAt:                issue.ClosedAt,
			LastIssueSyncAt:         issue.UpdatedAt,
		},
	}, nil
}

func parseSprintTitle(title string) (string, string, error) {
	match := sprintTitlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return "", "", newValidationError("invalid sprint title %q", title)
	}
	return match[1], strings.TrimSpace(match[2]), nil
}

func parseTaskTitle(title string) (string, string, string, error) {
	match := taskTitlePattern.FindStringSubmatch(strings.TrimSpace(title))
	if match == nil {
		return "", "", "", newValidationError("invalid task title %q", title)
	}
	return match[1], match[2], strings.TrimSpace(match[3]), nil
}

func parseSprintSequence(sprintID string) (int, error) {
	match := sprintIDPattern.FindStringSubmatch(strings.TrimSpace(sprintID))
	if match == nil {
		return 0, newValidationError("invalid sprint id %q", sprintID)
	}
	var seq int
	if _, err := fmt.Sscanf(match[1], "%d", &seq); err != nil {
		return 0, newValidationError("invalid sprint id %q", sprintID)
	}
	return seq, nil
}

func parseTaskSequence(taskID string) (int, error) {
	match := taskIDPattern.FindStringSubmatch(strings.TrimSpace(taskID))
	if match == nil {
		return 0, newValidationError("invalid task id %q", taskID)
	}
	var seq int
	if _, err := fmt.Sscanf(match[1], "%d", &seq); err != nil {
		return 0, newValidationError("invalid task id %q", taskID)
	}
	return seq, nil
}

func parseMarkdownSections(body string) map[string][]string {
	sections := make(map[string][]string)
	var current string
	for _, rawLine := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(rawLine, " \t")
		if match := markdownHeadingPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			current = strings.TrimSpace(match[1])
			if _, ok := sections[current]; !ok {
				sections[current] = []string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "<!--") {
			continue
		}
		sections[current] = append(sections[current], line)
	}
	return sections
}

func sectionText(sections map[string][]string, name string) string {
	lines := make([]string, 0, len(sections[name]))
	for _, line := range sections[name] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, trimmed)
	}
	text := strings.TrimSpace(strings.Join(lines, "\n"))
	return text
}

func sectionList(sections map[string][]string, name string) []string {
	var values []string
	for _, line := range sections[name] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "- "):
			values = append(values, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		case strings.HasPrefix(trimmed, "* "):
			values = append(values, strings.TrimSpace(strings.TrimPrefix(trimmed, "* ")))
		default:
			values = append(values, trimmed)
		}
	}
	return values
}

func parseTaskBranch(branch string) (string, string, bool) {
	branch = strings.TrimSpace(branch)
	if match := canonicalTaskBranchPattern.FindStringSubmatch(branch); match != nil {
		return match[1], formatTaskID(match[1], match[2]), true
	}
	if match := legacyTaskBranchPattern.FindStringSubmatch(branch); match != nil {
		return match[1], formatTaskID(match[1], match[2]), true
	}
	return "", "", false
}

func parseSprintBranch(branch string) (string, bool) {
	match := sprintBranchPattern.FindStringSubmatch(strings.TrimSpace(branch))
	if match == nil {
		return "", false
	}
	return match[1], true
}

func formatTaskID(sprintID, taskLocalID string) string {
	return sprintID + "/" + taskLocalID
}

func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func sortChangedEntities(values []ChangedEntity) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].EntityType != values[j].EntityType {
			return values[i].EntityType < values[j].EntityType
		}
		return values[i].EntityID < values[j].EntityID
	})
}
