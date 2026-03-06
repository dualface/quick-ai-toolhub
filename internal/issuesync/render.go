package issuesync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func RenderSprintIssue(sprint *Sprint) (title, body string) {
	title = fmt.Sprintf("[%s] %s", sprint.ID, sprint.Title)

	var b strings.Builder
	writeTextSection(&b, "Sprint ID", sprint.ID)
	writeTextSection(&b, "Goal", sprint.Goal)
	writeListSection(&b, "Done When", sprint.DoneWhen)

	var tasks []string
	for _, task := range sprint.TaskOrder {
		tasks = append(tasks, fmt.Sprintf("[%s] %s", task.TaskLocalID, task.Title))
	}
	writeListSection(&b, "Tasks", tasks)
	writeListSection(&b, "Notes", []string{
		fmt.Sprintf("Synced from `%s`.", sprint.Source),
	})
	fmt.Fprintf(&b, "<!-- toolhub:kind=sprint sprint_id=%s source=%s -->\n", sprint.ID, sprint.Source)

	return title, strings.TrimSpace(b.String())
}

func RenderTaskIssue(task *TaskBrief) (title, body string) {
	title = fmt.Sprintf("[%s][%s] %s", task.SprintID, task.TaskLocalID, task.Title)

	var b strings.Builder
	writeTextSection(&b, "Sprint ID", task.SprintID)
	writeTextSection(&b, "Task ID", task.TaskLocalID)
	writeTextSection(&b, "Goal", task.Goal)
	writeListSection(&b, "Reads", task.Reads)
	writeListSection(&b, "Dependencies", task.Dependencies)
	writeListSection(&b, "In Scope", task.InScope)
	writeListSection(&b, "Out of Scope", task.OutOfScope)
	writeListSection(&b, "Deliverables", task.Deliverables)
	writeListSection(&b, "Acceptance Criteria", task.AcceptanceCriteria)

	notes := append([]string(nil), task.Notes...)
	notes = append(notes, fmt.Sprintf("Synced from `%s`.", task.Source))
	writeListSection(&b, "Notes", notes)
	fmt.Fprintf(&b, "<!-- toolhub:kind=task sprint_id=%s task_id=%s task_local_id=%s source=%s -->\n", task.SprintID, task.TaskID, task.TaskLocalID, task.Source)

	return title, strings.TrimSpace(b.String())
}

func HashDocument(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\n---\n" + body))
	return hex.EncodeToString(sum[:])
}

func writeTextSection(b *strings.Builder, heading, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	fmt.Fprintf(b, "## %s\n\n", heading)
	fmt.Fprintf(b, "%s\n\n", value)
}

func writeListSection(b *strings.Builder, heading string, values []string) {
	if len(values) == 0 {
		return
	}

	fmt.Fprintf(b, "## %s\n\n", heading)

	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		fmt.Fprintf(b, "- %s\n", value)
	}
	b.WriteString("\n")
}
