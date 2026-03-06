package agentrun

import (
	"encoding/json"
	"fmt"
	"strings"

	"quick-ai-toolhub/internal/issuesync"
)

func buildPrompt(agentType AgentType, task *issuesync.TaskBrief, sprint *issuesync.Sprint, attempt int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are acting as the %s agent for task %s.\n\n", agentType, task.TaskID)
	fmt.Fprintf(&b, "Attempt: %d\n", attempt)
	fmt.Fprintf(&b, "Sprint: %s\n", sprint.ID)
	fmt.Fprintf(&b, "Sprint Goal: %s\n\n", sprint.Goal)

	b.WriteString("Read these repository files before doing substantive work:\n")
	for _, path := range buildReadList(task) {
		fmt.Fprintf(&b, "- %s\n", path)
	}
	b.WriteString("\n")

	b.WriteString("Task brief summary:\n")
	fmt.Fprintf(&b, "- Goal: %s\n", task.Goal)
	writePromptList(&b, "In Scope", task.InScope)
	writePromptList(&b, "Out of Scope", task.OutOfScope)
	writePromptList(&b, "Deliverables", task.Deliverables)
	writePromptList(&b, "Acceptance Criteria", task.AcceptanceCriteria)
	if len(task.Dependencies) > 0 {
		writePromptList(&b, "Dependencies", task.Dependencies)
	}
	b.WriteString("\n")

	switch agentType {
	case AgentDeveloper:
		b.WriteString("Role rules:\n")
		b.WriteString("- Implement the task end-to-end within scope.\n")
		b.WriteString("- Run the smallest relevant validation before finishing.\n")
	case AgentQA:
		b.WriteString("Role rules:\n")
		b.WriteString("- Validate the current implementation.\n")
		b.WriteString("- Focus on build, test, and lint behavior.\n")
		b.WriteString("- Do not make unrelated code changes.\n")
	case AgentReviewer:
		b.WriteString("Role rules:\n")
		b.WriteString("- Review the current state and report findings.\n")
		b.WriteString("- Do not modify files.\n")
	}
	b.WriteString("- Respect the repository rules in PROJECT-DEVELOPER-GUIDE.md.\n")
	b.WriteString("- Final output must be a single JSON object matching the provided schema.\n")
	b.WriteString("- Do not wrap the final JSON in markdown fences.\n")

	return b.String()
}

func buildReadList(task *issuesync.TaskBrief) []string {
	seen := map[string]struct{}{}
	var paths []string

	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	add("PROJECT-DEVELOPER-GUIDE.md")
	add(task.Source)
	for _, path := range task.Reads {
		add(strings.Trim(path, "`"))
	}

	return paths
}

func writePromptList(b *strings.Builder, heading string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "- %s:\n", heading)
	for _, value := range values {
		value = strings.TrimSpace(strings.Trim(value, "`"))
		if value == "" {
			continue
		}
		fmt.Fprintf(b, "  - %s\n", value)
	}
}

func resultSchemaJSON() ([]byte, error) {
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"status", "summary", "next_action"},
		"properties": map[string]any{
			"status": map[string]any{
				"type":      "string",
				"minLength": 1,
			},
			"summary": map[string]any{
				"type":      "string",
				"minLength": 1,
			},
			"next_action": map[string]any{
				"type":      "string",
				"minLength": 1,
			},
			"failure_fingerprint": map[string]any{
				"type": "string",
			},
			"artifact_refs": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"log":      map[string]any{"type": "string"},
					"worktree": map[string]any{"type": "string"},
					"patch":    map[string]any{"type": "string"},
					"report":   map[string]any{"type": "string"},
				},
			},
			"findings": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"reviewer_id":         map[string]any{"type": "string"},
						"lens":                map[string]any{"type": "string"},
						"severity":            map[string]any{"type": "string"},
						"confidence":          map[string]any{"type": "string"},
						"category":            map[string]any{"type": "string"},
						"file_refs":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"summary":             map[string]any{"type": "string"},
						"evidence":            map[string]any{"type": "string"},
						"finding_fingerprint": map[string]any{"type": "string"},
						"suggested_action":    map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	return json.MarshalIndent(schema, "", "  ")
}
