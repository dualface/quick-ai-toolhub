package agentrun

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quick-ai-toolhub/internal/issuesync"
)

type Executor struct {
	runner CommandRunner
	now    func() time.Time
	runID  func() string
}

func NewExecutor(runner CommandRunner) *Executor {
	return &Executor{
		runner: runner,
		now:    time.Now,
		runID:  defaultRunID,
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
	opts.ConfigFile = resolveAgainstWorkDir(opts.WorkDir, opts.ConfigFile)
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

	settings, err := loadAgentSettings(opts.WorkDir, opts.ConfigFile)
	if err != nil {
		return Result{}, err
	}
	if opts.Model == "" {
		opts.Model = settings.defaultModelFor(opts.AgentType)
	}
	roleInstructions, err := settings.roleInstructions(opts.AgentType, task, sprint, opts.Attempt, opts.Lens, opts.ContextRefs, opts.WorkDir)
	if err != nil {
		return Result{}, err
	}

	runDir := buildRunDir(outputRoot, opts.TaskID, opts.AgentType, opts.Lens, opts.Attempt, e.now().UTC(), e.runID())
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "create run dir: %v", err)
	}
	runnerOutputDir, err := buildRunnerOutputDir(opts.WorkDir, outputRoot, runDir, opts.AgentType)
	if err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "create runner output dir: %v", err)
	}

	prompt := buildPrompt(opts.AgentType, task, sprint, opts.Attempt, opts.Lens, opts.ContextRefs, opts.WorkDir, roleInstructions)
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

	lastMessagePath := filepath.Join(runnerOutputDir, "last-message.json")
	cmdReq, err := buildCommand(opts, prompt, schemaPath, lastMessagePath)
	if err != nil {
		return Result{}, err
	}
	cmdReq.WorkDir = opts.WorkDir
	cmdReq.Env, err = buildCommandEnv(opts.WorkDir, opts.AgentType, opts.IsolatedCodexHome)
	if err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "prepare command env: %v", err)
	}
	cmdReq.StdoutWriter = opts.StreamOutput
	cmdReq.StderrWriter = opts.StreamOutput
	cmdReq.ProgressWriter = opts.ProgressOutput
	envKeys := commandEnvKeys(opts)
	cmdReq.Metadata = CommandMetadata{
		Model:       effectiveModel(opts),
		Sandbox:     effectiveSandboxMode(opts),
		EnvKeys:     envKeys,
		EnvSnapshot: envSnapshot(cmdReq.Env, envKeys...),
	}

	runCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	stopHeartbeat := startProgressHeartbeat(runCtx, opts.ProgressOutput, 30*time.Second)
	defer stopHeartbeat()

	cmdResult, runErr := e.runner.Run(runCtx, cmdReq)

	stdoutPath := filepath.Join(runDir, "stdout.log")
	stderrPath := filepath.Join(runDir, "stderr.log")
	combinedLogPath := filepath.Join(runDir, "runner.log")
	reportPath := filepath.Join(runDir, "result.json")
	if err := os.WriteFile(stdoutPath, cmdResult.Stdout, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write stdout log: %v", err)
	}
	if err := os.WriteFile(stderrPath, cmdResult.Stderr, 0o644); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write stderr log: %v", err)
	}
	if err := writeCombinedLog(combinedLogPath, cmdResult.Stdout, cmdResult.Stderr); err != nil {
		return Result{}, wrapToolError(ErrorCodeArtifactWriteFailed, false, "write combined log: %v", err)
	}

	result := Result{
		Runner:    RunnerCodexExec,
		SessionID: extractSessionID(cmdResult.Stdout),
	}

	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.Status = "failed"
			result.Summary = "codex_exec timed out before producing a structured result"
			result.NextAction = "retry"
			result.FailureFingerprint = ErrorCodeRunnerTimeout
			if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
				return Result{}, err
			}
			return result, nil
		}

		if payload, sessionID, err := parseRunnerOutput(lastMessagePath, cmdResult.Stdout); err == nil {
			if err := validatePayload(payload); err == nil {
				result = resultFromPayload(payload, sessionID)
				if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
					return Result{}, err
				}
				return result, nil
			} else if toolErr, ok := err.(*ToolError); ok && toolErr.Code == ErrorCodeMalformedOutput {
				result.Status = "failed"
				result.Summary = "codex_exec did not return a valid structured result"
				result.NextAction = "retry"
				result.FailureFingerprint = ErrorCodeMalformedOutput
				if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
					return Result{}, err
				}
				return result, nil
			}
		} else if toolErr, ok := err.(*ToolError); ok && toolErr.Code == ErrorCodeMalformedOutput {
			result.Status = "failed"
			result.Summary = "codex_exec did not return a valid structured result"
			result.NextAction = "retry"
			result.FailureFingerprint = ErrorCodeMalformedOutput
			if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
				return Result{}, err
			}
			return result, nil
		}

		result.Status = "failed"
		result.Summary = runnerFailureSummary(cmdResult.Stderr)
		result.NextAction = "retry"
		result.FailureFingerprint = ErrorCodeRunnerExecution
		if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	payload, sessionID, err := parseRunnerOutput(lastMessagePath, cmdResult.Stdout)
	if err != nil {
		result.Status = "failed"
		result.Summary = "codex_exec did not return a valid structured result"
		result.NextAction = "retry"
		result.FailureFingerprint = ErrorCodeMalformedOutput
		if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
			return Result{}, err
		}
		return result, nil
	}
	if err := validatePayload(payload); err != nil {
		result.Status = "failed"
		result.Summary = "codex_exec did not return a valid structured result"
		result.NextAction = "retry"
		result.FailureFingerprint = ErrorCodeMalformedOutput
		if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	result = resultFromPayload(payload, sessionID)
	if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
		return Result{}, err
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
	if opts.ConfigFile == "" {
		opts.ConfigFile = defaultConfigFile
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}
	if opts.IsolatedCodexHome && opts.AgentType == AgentReviewer {
		return newToolError(ErrorCodeInvalidRequest, "isolated codex home is not supported for reviewer", false)
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
	args := []string{"codex"}
	if opts.Yolo {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "--ask-for-approval", "never")
		args = append(args, "--sandbox", codexSandbox(opts.AgentType))
	}
	for _, dir := range additionalWritableDirs(opts.WorkDir, schemaPath, lastMessagePath) {
		args = append(args, "--add-dir", dir)
	}
	args = append(args,
		"exec",
		"--cd", opts.WorkDir,
		"--output-schema", schemaPath,
		"--json",
		"-o", lastMessagePath,
		"-",
	)
	if opts.Model != "" {
		args = []string{"codex"}
		if opts.Yolo {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		} else {
			args = append(args, "--ask-for-approval", "never")
			args = append(args, "--sandbox", codexSandbox(opts.AgentType))
		}
		for _, dir := range additionalWritableDirs(opts.WorkDir, schemaPath, lastMessagePath) {
			args = append(args, "--add-dir", dir)
		}
		args = append(args,
			"--model", opts.Model,
			"exec",
			"--cd", opts.WorkDir,
			"--output-schema", schemaPath,
			"--json",
			"-o", lastMessagePath,
			"-",
		)
	}
	return CommandRequest{Args: args, Stdin: []byte(prompt)}, nil
}

func effectiveModel(opts RunOptions) string {
	if strings.TrimSpace(opts.Model) == "" {
		return "(runner default)"
	}
	return opts.Model
}

func effectiveSandboxMode(opts RunOptions) string {
	if opts.Yolo {
		return "dangerously-bypass-approvals-and-sandbox"
	}
	return codexSandbox(opts.AgentType)
}

func commandEnvKeys(opts RunOptions) []string {
	if opts.AgentType == AgentReviewer {
		return nil
	}

	keys := []string{"TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE"}
	if opts.IsolatedCodexHome {
		keys = append(keys, "HOME")
	}
	return keys
}

func buildCommandEnv(workdir string, agentType AgentType, isolatedCodexHome bool) ([]string, error) {
	env := os.Environ()
	if agentType == AgentReviewer {
		return env, nil
	}

	baseDir := filepath.Join(workdir, ".toolhub", "runtime")
	tmpDir := filepath.Join(baseDir, "tmp")
	goBuildDir := filepath.Join(baseDir, "go-build")
	goCacheDir := filepath.Join(baseDir, "go-cache")
	for _, dir := range []string{tmpDir, goBuildDir, goCacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	env = upsertEnv(env, "TMPDIR", tmpDir)
	env = upsertEnv(env, "TMP", tmpDir)
	env = upsertEnv(env, "TEMP", tmpDir)
	env = upsertEnv(env, "GOTMPDIR", goBuildDir)
	env = upsertEnv(env, "GOCACHE", goCacheDir)
	if isolatedCodexHome {
		homeDir := filepath.Join(baseDir, "home")
		if err := os.MkdirAll(homeDir, 0o755); err != nil {
			return nil, err
		}
		env = upsertEnv(env, "HOME", homeDir)
	}
	return env, nil
}

func runnerFailureSummary(stderr []byte) string {
	const defaultSummary = "codex_exec failed before producing a structured result"

	text := string(stderr)
	if strings.Contains(text, ".codex/tmp/arg0") && strings.Contains(text, "Permission denied") {
		return "codex_exec failed before producing a structured result; codex could not write its runtime temp dir under ~/.codex/tmp/arg0"
	}
	return defaultSummary
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
	if !payload.HasFailureFingerprint {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing failure_fingerprint", true)
	}
	if !payload.FailureFingerprintValid {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: invalid failure_fingerprint", true)
	}
	if !payload.HasArtifactRefs {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing artifact_refs", true)
	}
	if !payload.ArtifactRefsValid {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: invalid artifact_refs", true)
	}
	if !payload.HasFindings {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing findings", true)
	}
	if !payload.FindingsValid {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: invalid findings", true)
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
	if raw, ok := v["failure_fingerprint"]; ok {
		payload.HasFailureFingerprint = true
		payload.FailureFingerprintValid = true
		if raw != nil {
			value, ok := raw.(string)
			if !ok {
				payload.FailureFingerprintValid = false
			} else {
				payload.FailureFingerprint = value
			}
		}
	}
	if raw, ok := v["artifact_refs"]; ok {
		payload.HasArtifactRefs = true
		payload.ArtifactRefsValid = true
		switch refs := raw.(type) {
		case nil:
		case map[string]any:
			if !hasKeys(refs, "log", "worktree", "patch", "report") {
				payload.ArtifactRefsValid = false
			} else {
				logValue, logOK := getNullableString(refs, "log")
				worktreeValue, worktreeOK := getNullableString(refs, "worktree")
				patchValue, patchOK := getNullableString(refs, "patch")
				reportValue, reportOK := getNullableString(refs, "report")
				payload.ArtifactRefsValid = logOK && worktreeOK && patchOK && reportOK
				if payload.ArtifactRefsValid {
					payload.ArtifactRefs = ArtifactRefs{
						Log:      logValue,
						Worktree: worktreeValue,
						Patch:    patchValue,
						Report:   reportValue,
					}
				}
			}
		default:
			payload.ArtifactRefsValid = false
		}
	}
	if raw, ok := v["findings"]; ok {
		payload.HasFindings = true
		payload.FindingsValid = true
		switch findings := raw.(type) {
		case nil:
		case []any:
			for _, item := range findings {
				entry, ok := item.(map[string]any)
				if !ok || !hasKeys(entry, "reviewer_id", "lens", "severity", "confidence", "category", "file_refs", "summary", "evidence", "finding_fingerprint", "suggested_action") {
					payload.FindingsValid = false
					break
				}
				finding, ok := parseFinding(entry)
				if !ok {
					payload.FindingsValid = false
					break
				}
				payload.Findings = append(payload.Findings, finding)
			}
		default:
			payload.FindingsValid = false
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

func getNullableString(value map[string]any, key string) (string, bool) {
	raw, ok := value[key]
	if !ok {
		return "", false
	}
	if raw == nil {
		return "", true
	}
	str, ok := raw.(string)
	if !ok {
		return "", false
	}
	return str, true
}

func parseNullableStringSlice(value any) ([]string, bool) {
	if value == nil {
		return nil, true
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	var result []string
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			return nil, false
		}
		if strings.TrimSpace(str) != "" {
			result = append(result, str)
		}
	}
	return result, true
}

func parseFinding(entry map[string]any) (Finding, bool) {
	reviewerID, ok := getNullableString(entry, "reviewer_id")
	if !ok {
		return Finding{}, false
	}
	lens, ok := getNullableString(entry, "lens")
	if !ok {
		return Finding{}, false
	}
	severity, ok := getNullableString(entry, "severity")
	if !ok {
		return Finding{}, false
	}
	confidence, ok := getNullableString(entry, "confidence")
	if !ok {
		return Finding{}, false
	}
	category, ok := getNullableString(entry, "category")
	if !ok {
		return Finding{}, false
	}
	fileRefs, ok := parseNullableStringSlice(entry["file_refs"])
	if !ok {
		return Finding{}, false
	}
	summary, ok := getNullableString(entry, "summary")
	if !ok {
		return Finding{}, false
	}
	evidence, ok := getNullableString(entry, "evidence")
	if !ok {
		return Finding{}, false
	}
	fingerprint, ok := getNullableString(entry, "finding_fingerprint")
	if !ok {
		return Finding{}, false
	}
	suggestedAction, ok := getNullableString(entry, "suggested_action")
	if !ok {
		return Finding{}, false
	}

	return Finding{
		ReviewerID:         reviewerID,
		Lens:               lens,
		Severity:           severity,
		Confidence:         confidence,
		Category:           category,
		FileRefs:           fileRefs,
		Summary:            summary,
		Evidence:           evidence,
		FindingFingerprint: fingerprint,
		SuggestedAction:    suggestedAction,
	}, true
}

func hasKeys(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
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

func buildRunDir(outputRoot, taskID string, agentType AgentType, lens string, attempt int, now time.Time, runID string) string {
	if attempt <= 0 {
		attempt = 1
	}
	if strings.TrimSpace(lens) == "" {
		lens = "default"
	}
	runLeaf := fmt.Sprintf("%s-%s", now.Format("20060102T150405.000000000Z"), sanitizePathSegment(runID))
	return filepath.Join(
		outputRoot,
		filepath.FromSlash(taskID),
		sanitizePathSegment(string(agentType)),
		fmt.Sprintf("attempt-%02d", attempt),
		sanitizePathSegment(lens),
		runLeaf,
	)
}

func resultFromPayload(payload resultPayload, sessionID string) Result {
	return Result{
		Runner:             RunnerCodexExec,
		Status:             payload.Status,
		Summary:            payload.Summary,
		NextAction:         payload.NextAction,
		FailureFingerprint: payload.FailureFingerprint,
		SessionID:          sessionID,
		ArtifactRefs:       payload.ArtifactRefs,
		Findings:           payload.Findings,
	}
}

func persistResultReport(result *Result, workdir, combinedLogPath, reportPath string) error {
	applyDefaultArtifactRefs(result, workdir, combinedLogPath)
	if result.ArtifactRefs.Report == "" {
		result.ArtifactRefs.Report = relOrAbs(workdir, reportPath)
	}

	reportBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return wrapToolError(ErrorCodeInternalFailure, false, "marshal result: %v", err)
	}
	if err := os.WriteFile(reportPath, reportBytes, 0o644); err != nil {
		return wrapToolError(ErrorCodeArtifactWriteFailed, false, "write result report: %v", err)
	}
	return nil
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

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func envSnapshot(env []string, keys ...string) map[string]string {
	snapshot := make(map[string]string, len(keys))
	for _, key := range keys {
		prefix := key + "="
		for _, entry := range env {
			if strings.HasPrefix(entry, prefix) {
				snapshot[key] = strings.TrimPrefix(entry, prefix)
				break
			}
		}
	}
	return snapshot
}

func buildRunnerOutputDir(workdir, outputRoot, runDir string, agentType AgentType) (string, error) {
	if agentType != AgentReviewer || !isWithinWorkdir(workdir, runDir) {
		return runDir, nil
	}

	relRunPath, err := filepath.Rel(outputRoot, runDir)
	if err != nil {
		return "", err
	}
	runnerOutputDir := filepath.Join(os.TempDir(), "toolhub-codex", relRunPath)
	if err := os.MkdirAll(runnerOutputDir, 0o755); err != nil {
		return "", err
	}
	return runnerOutputDir, nil
}

func additionalWritableDirs(workdir string, paths ...string) []string {
	seen := make(map[string]struct{})
	var dirs []string
	for _, path := range paths {
		dir := filepath.Clean(filepath.Dir(path))
		if dir == "." || dir == "" {
			continue
		}
		if isWithinWorkdir(workdir, dir) {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return dirs
}

func isWithinWorkdir(workdir, path string) bool {
	rel, err := filepath.Rel(workdir, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func sanitizePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func defaultRunID() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
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

func startProgressHeartbeat(ctx context.Context, out io.Writer, interval time.Duration) func() {
	if out == nil || interval <= 0 {
		return func() {}
	}

	done := make(chan struct{})
	startedAt := time.Now()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(out, "[progress] still running (%s)\n", time.Since(startedAt).Round(time.Millisecond))
			case <-ctx.Done():
				return
			case <-done:
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}

func codexSandbox(agentType AgentType) string {
	if agentType == AgentReviewer {
		return "read-only"
	}
	return "workspace-write"
}
