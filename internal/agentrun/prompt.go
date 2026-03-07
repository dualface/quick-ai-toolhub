package agentrun

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"quick-ai-toolhub/internal/issuesync"
)

func buildPrompt(agentType AgentType, task *issuesync.TaskBrief, sprint *issuesync.Sprint, attempt int, lens string, contextRefs ContextRefs, workdir, roleInstructions string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are acting as the %s agent for task %s.\n\n", agentType, task.TaskID)
	fmt.Fprintf(&b, "Attempt: %d\n", attempt)
	fmt.Fprintf(&b, "Sprint: %s\n", sprint.ID)
	fmt.Fprintf(&b, "Sprint Goal: %s\n\n", sprint.Goal)
	if lens != "" {
		fmt.Fprintf(&b, "Lens: %s\n\n", lens)
	}

	b.WriteString("Read these repository files before doing substantive work:\n")
	for _, path := range buildReadList(task, workdir) {
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

	b.WriteString("Execution context:\n")
	fmt.Fprintf(&b, "- sprint_id: %s\n", contextRefs.SprintID)
	fmt.Fprintf(&b, "- worktree_path: %s\n", contextRefs.WorktreePath)
	if contextRefs.GitHubPRNumber > 0 {
		fmt.Fprintf(&b, "- github_pr_number: %d\n", contextRefs.GitHubPRNumber)
	}
	writeArtifactRefsSection(&b, "artifact_refs", contextRefs.ArtifactRefs)
	writeArtifactRefsSection(&b, "latest_qa_artifact_refs", contextRefs.QAArtifactRefs)
	writeFeedbackRefsSection(&b, "latest_qa_feedback", contextRefs.QAFeedback)
	writeArtifactRefsSection(&b, "latest_reviewer_artifact_refs", contextRefs.ReviewerArtifactRefs)
	writeFeedbackRefsSection(&b, "latest_reviewer_feedback", contextRefs.ReviewerFeedback)
	writeDeveloperRefsSection(&b, "previous_developer_context", contextRefs.PreviousDeveloper)
	b.WriteString("\n")

	if strings.TrimSpace(roleInstructions) == "" {
		roleInstructions = defaultRoleInstructions(agentType)
	}
	b.WriteString("Role instructions:\n")
	b.WriteString(roleInstructions)
	if !strings.HasSuffix(roleInstructions, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("- Respect the repository rules in PROJECT-DEVELOPER-GUIDE.md.\n")
	b.WriteString("- Final output must be a single JSON object matching the provided schema.\n")
	b.WriteString("- Do not omit schema fields; use null when a field is not applicable.\n")
	b.WriteString("- Do not wrap the final JSON in markdown fences.\n")

	return b.String()
}

func buildReadList(task *issuesync.TaskBrief, workdir string) []string {
	seen := map[string]struct{}{}
	var paths []string

	add := func(path string) {
		path = normalizeMarkdownCodeRef(path)
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
	add(relativeToWorkdir(workdir, task.Source))
	for _, path := range task.Reads {
		add(path)
	}

	return paths
}

func writePromptList(b *strings.Builder, heading string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "- %s:\n", heading)
	for _, value := range values {
		value = normalizeMarkdownCodeRef(value)
		if value == "" {
			continue
		}
		fmt.Fprintf(b, "  - %s\n", value)
	}
}

func normalizeMarkdownCodeRef(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, "`") && strings.HasSuffix(value, "`") {
		return strings.Trim(value, "`")
	}
	return value
}

func relativeToWorkdir(workdir, path string) string {
	if workdir == "" || !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(workdir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func writeArtifactRefsSection(b *strings.Builder, heading string, refs ArtifactRefs) {
	if !hasArtifactRefs(refs) {
		return
	}

	fmt.Fprintf(b, "- %s:\n", heading)
	if refs.Log != "" {
		fmt.Fprintf(b, "  - log: %s\n", refs.Log)
	}
	if refs.Worktree != "" {
		fmt.Fprintf(b, "  - worktree: %s\n", refs.Worktree)
	}
	if refs.Patch != "" {
		fmt.Fprintf(b, "  - patch: %s\n", refs.Patch)
	}
	if refs.Report != "" {
		fmt.Fprintf(b, "  - report: %s\n", refs.Report)
	}
}

func hasArtifactRefs(refs ArtifactRefs) bool {
	return refs.Log != "" || refs.Worktree != "" || refs.Patch != "" || refs.Report != ""
}

func writeFeedbackRefsSection(b *strings.Builder, heading string, refs FeedbackRefs) {
	if !hasFeedbackRefs(refs) {
		return
	}

	fmt.Fprintf(b, "- %s:\n", heading)
	if refs.Attempt > 0 {
		fmt.Fprintf(b, "  - attempt: %d\n", refs.Attempt)
	}
	if refs.Status != "" {
		fmt.Fprintf(b, "  - status: %s\n", refs.Status)
	}
	if refs.NextAction != "" {
		fmt.Fprintf(b, "  - next_action: %s\n", refs.NextAction)
	}
	if refs.FailureFingerprint != "" {
		fmt.Fprintf(b, "  - failure_fingerprint: %s\n", refs.FailureFingerprint)
	}
	if refs.Summary != "" {
		fmt.Fprintf(b, "  - summary: %s\n", normalizePromptText(refs.Summary))
	}
	if len(refs.Findings) == 0 {
		fmt.Fprintf(b, "  - findings: none\n")
		return
	}

	b.WriteString("  - findings:\n")
	for _, finding := range refs.Findings {
		fmt.Fprintf(
			b,
			"    - severity=%s confidence=%s lens=%s category=%s reviewer_id=%s\n",
			fallbackPromptValue(finding.Severity, "unknown"),
			fallbackPromptValue(finding.Confidence, "unknown"),
			fallbackPromptValue(finding.Lens, "unknown"),
			fallbackPromptValue(finding.Category, "unknown"),
			fallbackPromptValue(finding.ReviewerID, "unknown"),
		)
		if finding.Summary != "" {
			fmt.Fprintf(b, "      summary: %s\n", normalizePromptText(finding.Summary))
		}
		if len(finding.FileRefs) > 0 {
			fmt.Fprintf(b, "      file_refs: %s\n", strings.Join(finding.FileRefs, ", "))
		}
		if finding.FindingFingerprint != "" {
			fmt.Fprintf(b, "      finding_fingerprint: %s\n", finding.FindingFingerprint)
		}
		if finding.SuggestedAction != "" {
			fmt.Fprintf(b, "      suggested_action: %s\n", normalizePromptText(finding.SuggestedAction))
		}
	}
}

func writeDeveloperRefsSection(b *strings.Builder, heading string, refs DeveloperRefs) {
	if !hasDeveloperRefs(refs) {
		return
	}

	fmt.Fprintf(b, "- %s:\n", heading)
	if refs.Attempt > 0 {
		fmt.Fprintf(b, "  - attempt: %d\n", refs.Attempt)
	}
	if refs.Status != "" {
		fmt.Fprintf(b, "  - status: %s\n", refs.Status)
	}
	if refs.NextAction != "" {
		fmt.Fprintf(b, "  - next_action: %s\n", refs.NextAction)
	}
	if refs.Summary != "" {
		fmt.Fprintf(b, "  - summary: %s\n", normalizePromptText(refs.Summary))
	}
	if len(refs.ChangedFiles) == 0 {
		return
	}

	b.WriteString("  - changed_files:\n")
	for _, path := range refs.ChangedFiles {
		fmt.Fprintf(b, "    - %s\n", path)
	}
}

func normalizePromptText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func fallbackPromptValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultRoleInstructions(agentType AgentType) string {
	switch agentType {
	case AgentDeveloper:
		return strings.Join([]string{
			"- Implement the task end-to-end within scope.",
			"- If execution context includes latest_qa_feedback, read it first, then use latest_qa_artifact_refs for full detail before making changes.",
			"- Fix the concrete problems called out by that latest QA round before doing any follow-on work.",
			"- After the latest QA issues are addressed, read latest_reviewer_feedback, then use latest_reviewer_artifact_refs to fix the latest reviewer findings.",
			"- If previous_developer_context is present, continue from that summary and changed file list instead of re-discovering the same work.",
			"- After fixing the explicit findings, inspect adjacent branches in the same control flow, persistence path, and recovery path for similar defects.",
			"- Run the smallest validation that proves both the reported issue and the adjacent paths are covered before finishing.",
		}, "\n")
	case AgentQA:
		return strings.Join([]string{
			"- Validate the current implementation.",
			"- Focus on build, test, and lint behavior.",
			"- Use the provided repo-local temp/cache environment for Go commands instead of relying on /tmp defaults.",
			"- Prefer repository-defined validation commands; do not block solely because a global lint tool is absent unless the repository explicitly requires it.",
			"- If environment limits prevent a check from running, report that as a verification gap, not as a code defect.",
			"- Do not make unrelated code changes.",
		}, "\n")
	case AgentReviewer:
		return strings.Join([]string{
			"- Review the current state and report findings.",
			"- Do not modify files.",
		}, "\n")
	default:
		return ""
	}
}

func resultSchemaJSON() ([]byte, error) {
	requiredString := func(extra map[string]any) map[string]any {
		schema := map[string]any{
			"type":    "string",
			"pattern": `.*\S.*`,
		}
		for key, value := range extra {
			schema[key] = value
		}
		return schema
	}

	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"status",
			"summary",
			"next_action",
			"failure_fingerprint",
			"artifact_refs",
			"findings",
		},
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": allowedResultStatuses(),
			},
			"summary": map[string]any{
				"type": "string",
			},
			"next_action": map[string]any{
				"type": "string",
			},
			"failure_fingerprint": map[string]any{
				"type": []string{"string", "null"},
			},
			"artifact_refs": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "null"},
					map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"log", "worktree", "patch", "report"},
						"properties": map[string]any{
							"log":      map[string]any{"type": []string{"string", "null"}},
							"worktree": map[string]any{"type": []string{"string", "null"}},
							"patch":    map[string]any{"type": []string{"string", "null"}},
							"report":   map[string]any{"type": []string{"string", "null"}},
						},
					},
				},
			},
			"findings": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "null"},
					map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required": []string{
								"reviewer_id",
								"lens",
								"severity",
								"confidence",
								"category",
								"file_refs",
								"summary",
								"evidence",
								"finding_fingerprint",
								"suggested_action",
							},
							"properties": map[string]any{
								"reviewer_id": requiredString(nil),
								"lens": requiredString(map[string]any{
									"enum": AllowedReviewerLenses(),
								}),
								"severity": requiredString(map[string]any{
									"enum": AllowedFindingSeverities(),
								}),
								"confidence": requiredString(map[string]any{
									"enum": AllowedFindingConfidences(),
								}),
								"category": requiredString(nil),
								"file_refs": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
								"summary":             requiredString(nil),
								"evidence":            requiredString(nil),
								"finding_fingerprint": requiredString(nil),
								"suggested_action":    requiredString(nil),
							},
						},
					},
				},
			},
		},
	}
	return json.MarshalIndent(schema, "", "  ")
}
