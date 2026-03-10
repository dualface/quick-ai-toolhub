package issuesync

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Parser struct{}

func (Parser) Load(planFile, tasksDir string) (*PlanData, error) {
	sprints, err := parseSprintPlan(planFile)
	if err != nil {
		return nil, err
	}

	taskBriefs, err := loadTaskBriefs(tasksDir)
	if err != nil {
		return nil, err
	}

	plan := &PlanData{
		Sprints: sprints,
		Tasks:   taskBriefs,
	}

	if err := validatePlan(plan); err != nil {
		return nil, err
	}

	return plan, nil
}

func (Parser) LoadTask(tasksDir, taskID string) (*TaskBrief, *Sprint, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil, fmt.Errorf("task id is required")
	}

	taskBriefs, err := loadTaskBriefs(tasksDir)
	if err != nil {
		return nil, nil, err
	}

	task, ok := taskBriefs[taskID]
	if !ok {
		return nil, nil, fmt.Errorf("task %s not found", taskID)
	}

	return task, &Sprint{
		ID:     task.SprintID,
		Source: filepath.ToSlash(tasksDir),
	}, nil
}

func parseSprintPlan(path string) ([]*Sprint, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sprint plan %s: %w", path, err)
	}

	lines := splitLines(string(content))
	var sprints []*Sprint

	for idx := 0; idx < len(lines); idx++ {
		match := sprintHeadingPattern.FindStringSubmatch(lines[idx])
		if match == nil {
			continue
		}

		start := idx + 1
		end := len(lines)
		for j := start; j < len(lines); j++ {
			if sprintHeadingPattern.MatchString(lines[j]) {
				end = j
				break
			}
		}

		sections := parseSections(lines[start:end], "### ")
		tasks, err := parseTaskTable(sections["Tasks"])
		if err != nil {
			return nil, fmt.Errorf("parse tasks for %s: %w", match[1], err)
		}

		sprints = append(sprints, &Sprint{
			ID:        match[1],
			Title:     strings.TrimSpace(match[2]),
			Goal:      parseParagraph(sections["Goal"]),
			DoneWhen:  parseList(sections["Done When"]),
			TaskOrder: tasks,
			Source:    filepath.ToSlash(path),
		})

		idx = end - 1
	}

	if len(sprints) == 0 {
		return nil, fmt.Errorf("no Sprint sections found in %s", path)
	}

	return sprints, nil
}

func loadTaskBriefs(dir string) (map[string]*TaskBrief, error) {
	files, err := collectMarkdownFiles(dir)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*TaskBrief, len(files))
	for _, path := range files {
		brief, err := parseTaskBrief(path)
		if err != nil {
			return nil, err
		}
		result[brief.TaskID] = brief
	}

	return result, nil
}

func collectMarkdownFiles(dir string) ([]string, error) {
	var files []string
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".md" {
			files = append(files, filepath.ToSlash(path))
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk task briefs %s: %w", dir, err)
	}

	slices.Sort(files)
	return files, nil
}

func parseTaskBrief(path string) (*TaskBrief, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task brief %s: %w", path, err)
	}

	lines := splitLines(string(content))
	if len(lines) == 0 {
		return nil, fmt.Errorf("task brief %s is empty", path)
	}

	match := taskHeadingPattern.FindStringSubmatch(lines[0])
	if match == nil {
		return nil, fmt.Errorf("task brief %s has invalid title line", path)
	}

	sections := parseSections(lines[1:], "## ")
	taskLocalID := match[2]
	sprintID := match[1]

	return &TaskBrief{
		SprintID:           sprintID,
		TaskLocalID:        taskLocalID,
		TaskID:             fmt.Sprintf("%s/%s", sprintID, taskLocalID),
		Title:              strings.TrimSpace(match[3]),
		Goal:               parseParagraph(sections["Goal"]),
		Reads:              parseList(sections["Reads"]),
		Dependencies:       parseList(sections["Dependencies"]),
		InScope:            parseList(sections["In Scope"]),
		OutOfScope:         parseList(sections["Out of Scope"]),
		Deliverables:       parseList(sections["Deliverables"]),
		AcceptanceCriteria: parseList(sections["Acceptance Criteria"]),
		Notes:              parseList(sections["Notes"]),
		Source:             filepath.ToSlash(path),
	}, nil
}

func validatePlan(plan *PlanData) error {
	seenTaskIDs := make(map[string]struct{})

	for _, sprint := range plan.Sprints {
		if sprint.Goal == "" {
			return fmt.Errorf("%s is missing Goal", sprint.ID)
		}
		if len(sprint.DoneWhen) == 0 {
			return fmt.Errorf("%s is missing Done When", sprint.ID)
		}
		for _, summary := range sprint.TaskOrder {
			taskID := fmt.Sprintf("%s/%s", sprint.ID, summary.TaskLocalID)
			brief, ok := plan.Tasks[taskID]
			if !ok {
				return fmt.Errorf("missing task brief for %s", taskID)
			}
			if summary.Title != brief.Title {
				return fmt.Errorf("task title mismatch for %s: plan=%q brief=%q", taskID, summary.Title, brief.Title)
			}
			seenTaskIDs[taskID] = struct{}{}
		}
	}

	for taskID := range plan.Tasks {
		if _, ok := seenTaskIDs[taskID]; !ok {
			return fmt.Errorf("task brief %s is not referenced by plan/SPRINTS-V1.md", taskID)
		}
	}

	return nil
}

func parseSections(lines []string, headingPrefix string) map[string][]string {
	sections := make(map[string][]string)
	var current string
	currentLevel := headingPrefixLevel(headingPrefix)
	for _, line := range lines {
		if strings.HasPrefix(line, headingPrefix) {
			current = strings.TrimSpace(strings.TrimPrefix(line, headingPrefix))
			sections[current] = nil
			continue
		}
		if current != "" {
			if level := headingLevel(line); level > 0 && level <= currentLevel {
				current = ""
				continue
			}
		}
		if current == "" {
			continue
		}
		sections[current] = append(sections[current], line)
	}
	return sections
}

func headingLevel(line string) int {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "#") {
		return 0
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

func headingPrefixLevel(prefix string) int {
	level := 0
	for level < len(prefix) && prefix[level] == '#' {
		level++
	}
	return level
}

func parseTaskTable(lines []string) ([]TaskSummary, error) {
	var rows []TaskSummary
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.Contains(line, "task_id") || strings.Contains(line, "---") {
			continue
		}

		cols := splitMarkdownRow(line)
		if len(cols) < 2 {
			return nil, fmt.Errorf("invalid task table row %q", line)
		}
		rows = append(rows, TaskSummary{
			TaskLocalID: strings.Trim(cols[0], "` "),
			Title:       strings.Trim(cols[1], "` "),
		})
	}

	return rows, nil
}

func splitMarkdownRow(row string) []string {
	trimmed := strings.TrimSpace(strings.Trim(row, "|"))
	parts := strings.Split(trimmed, "|")
	var cols []string
	for _, part := range parts {
		cols = append(cols, strings.TrimSpace(part))
	}
	return cols
}

func parseParagraph(lines []string) string {
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func parseList(lines []string) []string {
	var items []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if strings.EqualFold(item, "none") {
			continue
		}
		items = append(items, item)
	}
	return items
}

func splitLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.Split(content, "\n")
}
