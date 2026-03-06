package agentrun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quick-ai-toolhub/internal/issuesync"
)

type Executor struct {
	runner CommandRunner
	now    func() time.Time
}

func NewExecutor(runner CommandRunner) *Executor {
	return &Executor{
		runner: runner,
		now:    time.Now,
	}
}

func (e *Executor) RunTask(ctx context.Context, opts RunOptions) (Result, error) {
	if err := validateOptions(&opts); err != nil {
		return Result{}, err
	}

	absWorkDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = absWorkDir
	opts.PlanFile = resolveAgainstWorkDir(opts.WorkDir, opts.PlanFile)
	opts.TasksDir = resolveAgainstWorkDir(opts.WorkDir, opts.TasksDir)

	outputRoot := opts.OutputRoot
	if outputRoot == "" {
		outputRoot = ".toolhub/runs"
	}
	if !filepath.IsAbs(outputRoot) {
		outputRoot = filepath.Join(opts.WorkDir, outputRoot)
	}

	parser := issuesync.Parser{}
	planData, err := parser.Load(opts.PlanFile, opts.TasksDir)
	if err != nil {
		return Result{}, wrapToolError(ErrorCodePlanLoadFailed, false, "load plan data: %v", err)
	}

	task, sprint, err := findTask(planData, opts.TaskID)
	if err != nil {
		return Result{}, err
	}
	applyDefaultContextRefs(&opts, sprint)

	runDir := filepath.Join(outputRoot, filepath.FromSlash(opts.TaskID), e.now().UTC().Format("20060102T150405Z"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "create run dir: %v", err)
	}

	prompt := buildPrompt(opts.AgentType, task, sprint, opts.Attempt, opts.Lens, opts.ContextRefs, opts.WorkDir)
	promptPath := filepath.Join(runDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodePromptBuildFailed, false, "write prompt: %v", err)
	}

	schemaBytes, err := resultSchemaJSON()
	if err != nil {
		return Result{}, wrapToolError(ErrorCodeSchemaBuildFailed, false, "build schema: %v", err)
	}
	schemaPath := filepath.Join(runDir, "output-schema.json")
	if err := os.WriteFile(schemaPath, schemaBytes, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write schema: %v", err)
	}

	lastMessagePath := filepath.Join(runDir, "last-message.json")
	cmdReq, err := buildCommand(opts, prompt, schemaPath, lastMessagePath)
	if err != nil {
		return Result{}, err
	}
	cmdReq.WorkDir = opts.WorkDir
	cmdReq.StdoutWriter = opts.StreamOutput
	cmdReq.StderrWriter = opts.StreamOutput

	runCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmdResult, runErr := e.runner.Run(runCtx, cmdReq)

	stdoutPath := filepath.Join(runDir, "stdout.log")
	stderrPath := filepath.Join(runDir, "stderr.log")
	combinedLogPath := filepath.Join(runDir, "runner.log")
	if err := os.WriteFile(stdoutPath, cmdResult.Stdout, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write stdout log: %v", err)
	}
	if err := os.WriteFile(stderrPath, cmdResult.Stderr, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write stderr log: %v", err)
	}
	if err := writeCombinedLog(combinedLogPath, cmdResult.Stdout, cmdResult.Stderr); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write combined log: %v", err)
	}

	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return Result{}, wrapToolError(ErrorCodeRunnerTimeout, true, "runner timeout: %v", runErr)
		}
		return Result{}, wrapToolError(ErrorCodeRunnerExecution, true, "runner execution failed: %v", runErr)
	}

	payload, sessionID, err := parseRunnerOutput(lastMessagePath, cmdResult.Stdout)
	if err != nil {
		return Result{}, err
	}
	if err := validatePayload(payload); err != nil {
		return Result{}, err
	}

	result := Result{
		Runner:             RunnerCodexExec,
		Status:             payload.Status,
		Summary:            payload.Summary,
		NextAction:         payload.NextAction,
		FailureFingerprint: payload.FailureFingerprint,
		SessionID:          sessionID,
		ArtifactRefs:       payload.ArtifactRefs,
		Findings:           payload.Findings,
	}
	applyDefaultArtifactRefs(&result, opts.WorkDir, combinedLogPath)

	reportPath := filepath.Join(runDir, "result.json")
	reportBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return Result{}, wrapToolError(ErrorCodeInternalFailure, false, "marshal result: %v", err)
	}
	if err := os.WriteFile(reportPath, reportBytes, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write result report: %v", err)
	}
	result.ArtifactRefs.Report = relOrAbs(opts.WorkDir, reportPath)

	reportBytes, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		return Result{}, wrapToolError(ErrorCodeInternalFailure, false, "marshal final result: %v", err)
	}
	if err := os.WriteFile(reportPath, reportBytes, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "rewrite result report: %v", err)
	}

	return result, nil
}

func validateOptions(opts *RunOptions) error {
	if opts.TaskID == "" {
		return newToolError(ErrorCodeInvalidRequest, "missing task id", false)
	}
	if opts.AgentType == "" {
		opts.AgentType = AgentDeveloper
	}
	switch opts.AgentType {
	case AgentDeveloper, AgentQA, AgentReviewer:
	default:
		return wrapToolError(ErrorCodeInvalidRequest, false, "unsupported agent type %q", opts.AgentType)
	}

	if opts.Attempt <= 0 {
		opts.Attempt = 1
	}
	if opts.PlanFile == "" {
		opts.PlanFile = "plan/SPRINTS-V1.md"
	}
	if opts.TasksDir == "" {
		opts.TasksDir = "plan/tasks"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}
	return nil
}

func findTask(plan *issuesync.PlanData, taskID string) (*issuesync.TaskBrief, *issuesync.Sprint, error) {
	task, ok := plan.Tasks[taskID]
	if !ok {
		return nil, nil, wrapToolError(ErrorCodeTaskNotFound, false, "task %s not found", taskID)
	}
	for _, sprint := range plan.Sprints {
		if sprint.ID == task.SprintID {
			return task, sprint, nil
		}
	}
	return nil, nil, wrapToolError(ErrorCodeTaskNotFound, false, "sprint %s not found for task %s", task.SprintID, taskID)
}

func applyDefaultContextRefs(opts *RunOptions, sprint *issuesync.Sprint) {
	if opts.ContextRefs.SprintID == "" {
		opts.ContextRefs.SprintID = sprint.ID
	}
	if opts.ContextRefs.WorktreePath == "" {
		opts.ContextRefs.WorktreePath = opts.WorkDir
	}
	if opts.ContextRefs.ArtifactRefs.Worktree == "" {
		opts.ContextRefs.ArtifactRefs.Worktree = "."
	}
}

func buildCommand(opts RunOptions, prompt, schemaPath, lastMessagePath string) (CommandRequest, error) {
	args := []string{
		"codex",
		"--ask-for-approval", "never",
		"--sandbox", codexSandbox(opts.AgentType),
		"exec",
		"--cd", opts.WorkDir,
		"--output-schema", schemaPath,
		"--json",
		"-o", lastMessagePath,
		"-",
	}
	if opts.Model != "" {
		args = []string{
			"codex",
			"--ask-for-approval", "never",
			"--sandbox", codexSandbox(opts.AgentType),
			"--model", opts.Model,
			"exec",
			"--cd", opts.WorkDir,
			"--output-schema", schemaPath,
			"--json",
			"-o", lastMessagePath,
			"-",
		}
	}
	return CommandRequest{Args: args, Stdin: []byte(prompt)}, nil
}

func parseRunnerOutput(lastMessagePath string, stdout []byte) (resultPayload, string, error) {
	if data, err := os.ReadFile(lastMessagePath); err == nil {
		if payload, ok := tryDecodePayloadBytes(data); ok {
			return payload, extractSessionID(stdout), nil
		}
	}

	payload, ok := tryDecodePayloadBytes(stdout)
	if !ok {
		return resultPayload{}, "", newToolError(ErrorCodeMalformedOutput, "malformed_output: could not decode codex result", true)
	}
	return payload, extractSessionID(stdout), nil
}

func validatePayload(payload resultPayload) error {
	if strings.TrimSpace(payload.Status) == "" {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing status", true)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing summary", true)
	}
	if strings.TrimSpace(payload.NextAction) == "" {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing next_action", true)
	}
	return nil
}

func tryDecodePayloadBytes(raw []byte) (resultPayload, bool) {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return resultPayload{}, false
	}

	if payload, ok := decodePayload(raw); ok {
		return payload, true
	}

	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	var last resultPayload
	var found bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if payload, ok := decodePayload([]byte(line)); ok {
			last = payload
			found = true
		}
	}
	return last, found
}

func decodePayload(raw []byte) (resultPayload, bool) {
	var payload resultPayload
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Status != "" && payload.Summary != "" && payload.NextAction != "" {
		return payload, true
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return resultPayload{}, false
	}
	return extractPayload(value)
}

func extractPayload(value any) (resultPayload, bool) {
	switch v := value.(type) {
	case map[string]any:
		if payload, ok := payloadFromMap(v); ok {
			return payload, true
		}
		for _, child := range v {
			if payload, ok := extractPayload(child); ok {
				return payload, true
			}
		}
	case []any:
		for i := len(v) - 1; i >= 0; i-- {
			if payload, ok := extractPayload(v[i]); ok {
				return payload, true
			}
		}
	case string:
		return tryDecodePayloadBytes([]byte(v))
	}
	return resultPayload{}, false
}

func payloadFromMap(v map[string]any) (resultPayload, bool) {
	status, ok1 := getString(v, "status")
	summary, ok2 := getString(v, "summary")
	nextAction, ok3 := getString(v, "next_action")
	if !(ok1 && ok2 && ok3) {
		return resultPayload{}, false
	}

	payload := resultPayload{
		Status:     status,
		Summary:    summary,
		NextAction: nextAction,
	}
	if value, ok := getString(v, "failure_fingerprint"); ok {
		payload.FailureFingerprint = value
	}
	if refs, ok := v["artifact_refs"].(map[string]any); ok {
		payload.ArtifactRefs = ArtifactRefs{
			Log:      getStringOrEmpty(refs, "log"),
			Worktree: getStringOrEmpty(refs, "worktree"),
			Patch:    getStringOrEmpty(refs, "patch"),
			Report:   getStringOrEmpty(refs, "report"),
		}
	}
	if findings, ok := v["findings"].([]any); ok {
		for _, item := range findings {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			payload.Findings = append(payload.Findings, Finding{
				ReviewerID:         getStringOrEmpty(entry, "reviewer_id"),
				Lens:               getStringOrEmpty(entry, "lens"),
				Severity:           getStringOrEmpty(entry, "severity"),
				Confidence:         getStringOrEmpty(entry, "confidence"),
				Category:           getStringOrEmpty(entry, "category"),
				FileRefs:           getStringSlice(entry["file_refs"]),
				Summary:            getStringOrEmpty(entry, "summary"),
				Evidence:           getStringOrEmpty(entry, "evidence"),
				FindingFingerprint: getStringOrEmpty(entry, "finding_fingerprint"),
				SuggestedAction:    getStringOrEmpty(entry, "suggested_action"),
			})
		}
	}
	return payload, true
}

func getString(value map[string]any, key string) (string, bool) {
	raw, ok := value[key]
	if !ok {
		return "", false
	}
	str, ok := raw.(string)
	if !ok || strings.TrimSpace(str) == "" {
		return "", false
	}
	return str, true
}

func getStringOrEmpty(value map[string]any, key string) string {
	str, _ := getString(value, key)
	return str
}

func getStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, item := range items {
		str, ok := item.(string)
		if ok && strings.TrimSpace(str) != "" {
			result = append(result, str)
		}
	}
	return result
}

func extractSessionID(raw []byte) string {
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		if id := findSessionID(value); id != "" {
			return id
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		var line any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if id := findSessionID(line); id != "" {
			return id
		}
	}
	return ""
}

func findSessionID(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"session_id", "conversation_id", "thread_id", "id"} {
			if str, ok := v[key].(string); ok && looksLikeSessionID(str) {
				return str
			}
		}
		for _, child := range v {
			if id := findSessionID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range v {
			if id := findSessionID(child); id != "" {
				return id
			}
		}
	case string:
		if looksLikeSessionID(v) {
			return v
		}
	}
	return ""
}

func looksLikeSessionID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Count(value, "-") >= 3 && len(value) >= 16
}

func applyDefaultArtifactRefs(result *Result, workdir, combinedLogPath string) {
	if result.ArtifactRefs.Log == "" {
		result.ArtifactRefs.Log = relOrAbs(workdir, combinedLogPath)
	}
	if result.ArtifactRefs.Worktree == "" {
		result.ArtifactRefs.Worktree = "."
	}
}

func relOrAbs(workdir, path string) string {
	rel, err := filepath.Rel(workdir, path)
	if err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func resolveAgainstWorkDir(workdir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workdir, path)
}

func writeCombinedLog(path string, stdout, stderr []byte) error {
	var b strings.Builder
	b.WriteString("== stdout ==\n")
	b.Write(stdout)
	if len(stdout) > 0 && stdout[len(stdout)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("== stderr ==\n")
	b.Write(stderr)
	if len(stderr) > 0 && stderr[len(stderr)-1] != '\n' {
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func mustSchema(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return data
}

func codexSandbox(agentType AgentType) string {
	if agentType == AgentReviewer {
		return "read-only"
	}
	return "workspace-write"
}
