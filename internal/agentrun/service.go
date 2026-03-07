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
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quick-ai-toolhub/internal/issuesync"
)

const progressHeartbeatInterval = 60 * time.Second

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
	applyAutomaticFeedbackContextRefs(&opts, outputRoot)

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
	stopHeartbeat := startProgressHeartbeat(runCtx, opts.ProgressOutput, progressHeartbeatInterval)
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
			result.Status = "timeout"
			result.Summary = "codex_exec timed out before producing a structured result"
			result.NextAction = "retry"
			result.FailureFingerprint = ErrorCodeRunnerTimeout
			if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
				return Result{}, err
			}
			return result, nil
		}

		if payload, _, err := parseRunnerOutput(lastMessagePath, cmdResult.Stdout); err == nil {
			if err := validatePayload(payload); err != nil {
				if toolErr, ok := err.(*ToolError); ok && toolErr.Code == ErrorCodeMalformedOutput {
					result.Status = "failed"
					result.Summary = "codex_exec did not return a valid structured result"
					result.NextAction = "retry"
					result.FailureFingerprint = ErrorCodeMalformedOutput
					if err := persistResultReport(&result, opts.WorkDir, combinedLogPath, reportPath); err != nil {
						return Result{}, err
					}
					return result, nil
				}
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

func applyAutomaticFeedbackContextRefs(opts *RunOptions, outputRoot string) {
	if opts.AgentType != AgentDeveloper {
		return
	}

	if run, ok := findLatestUsableAgentRun(opts.WorkDir, outputRoot, opts.TaskID, AgentQA); ok {
		opts.ContextRefs.QAArtifactRefs = fillMissingArtifactRefs(opts.ContextRefs.QAArtifactRefs, run.ArtifactRefs)
		opts.ContextRefs.QAFeedback = fillMissingFeedbackRefs(opts.ContextRefs.QAFeedback, feedbackRefsFromHistoricalRun(run))
	}
	if run, ok := findLatestUsableAgentRun(opts.WorkDir, outputRoot, opts.TaskID, AgentReviewer); ok {
		opts.ContextRefs.ReviewerArtifactRefs = fillMissingArtifactRefs(opts.ContextRefs.ReviewerArtifactRefs, run.ArtifactRefs)
		opts.ContextRefs.ReviewerFeedback = fillMissingFeedbackRefs(opts.ContextRefs.ReviewerFeedback, feedbackRefsFromHistoricalRun(run))
	}
	if run, ok := findLatestUsableAgentRun(opts.WorkDir, outputRoot, opts.TaskID, AgentDeveloper); ok {
		opts.ContextRefs.PreviousDeveloper = fillMissingDeveloperRefs(
			opts.ContextRefs.PreviousDeveloper,
			developerRefsFromHistoricalRun(run, listWorktreeChangedFiles(opts.WorkDir)),
		)
	}
}

func fillMissingArtifactRefs(current, defaults ArtifactRefs) ArtifactRefs {
	if current.Log == "" {
		current.Log = defaults.Log
	}
	if current.Worktree == "" {
		current.Worktree = defaults.Worktree
	}
	if current.Patch == "" {
		current.Patch = defaults.Patch
	}
	if current.Report == "" {
		current.Report = defaults.Report
	}
	return current
}

func fillMissingFeedbackRefs(current, defaults FeedbackRefs) FeedbackRefs {
	if current.Attempt == 0 {
		current.Attempt = defaults.Attempt
	}
	if current.Status == "" {
		current.Status = defaults.Status
	}
	if current.NextAction == "" {
		current.NextAction = defaults.NextAction
	}
	if current.FailureFingerprint == "" {
		current.FailureFingerprint = defaults.FailureFingerprint
	}
	if current.Summary == "" {
		current.Summary = defaults.Summary
	}
	if len(current.Findings) == 0 && len(defaults.Findings) > 0 {
		current.Findings = append([]Finding(nil), defaults.Findings...)
	}
	return current
}

func fillMissingDeveloperRefs(current, defaults DeveloperRefs) DeveloperRefs {
	if current.Attempt == 0 {
		current.Attempt = defaults.Attempt
	}
	if current.Status == "" {
		current.Status = defaults.Status
	}
	if current.NextAction == "" {
		current.NextAction = defaults.NextAction
	}
	if current.Summary == "" {
		current.Summary = defaults.Summary
	}
	if len(current.ChangedFiles) == 0 && len(defaults.ChangedFiles) > 0 {
		current.ChangedFiles = append([]string(nil), defaults.ChangedFiles...)
	}
	return current
}

type historicalAgentRun struct {
	Attempt      int
	RunLeaf      string
	Result       Result
	ArtifactRefs ArtifactRefs
}

func findLatestUsableAgentRun(workdir, outputRoot, taskID string, agentType AgentType) (historicalAgentRun, bool) {
	attemptDirs, err := filepath.Glob(filepath.Join(outputRoot, filepath.FromSlash(taskID), sanitizePathSegment(string(agentType)), "attempt-*"))
	if err != nil {
		return historicalAgentRun{}, false
	}

	best := historicalAgentRun{Attempt: -1}
	for _, attemptDir := range attemptDirs {
		attempt, ok := parseAttemptDirName(filepath.Base(attemptDir))
		if !ok {
			continue
		}

		reportPaths, err := filepath.Glob(filepath.Join(attemptDir, "*", "*", "result.json"))
		if err != nil {
			continue
		}
		for _, reportPath := range reportPaths {
			info, err := os.Stat(reportPath)
			if err != nil || info.IsDir() {
				continue
			}

			runLeaf := filepath.Base(filepath.Dir(reportPath))
			result, err := readHistoricalRunResult(reportPath)
			if err != nil || !isUsableFeedbackResult(result) {
				continue
			}

			if attempt > best.Attempt || (attempt == best.Attempt && runLeaf > best.RunLeaf) {
				best = historicalAgentRun{
					Attempt: attempt,
					RunLeaf: runLeaf,
					Result:  result,
					ArtifactRefs: ArtifactRefs{
						Report: relOrAbs(workdir, reportPath),
					},
				}

				logPath := filepath.Join(filepath.Dir(reportPath), "runner.log")
				if logInfo, err := os.Stat(logPath); err == nil && !logInfo.IsDir() {
					best.ArtifactRefs.Log = relOrAbs(workdir, logPath)
				}
			}
		}
	}

	if best.Attempt <= 0 {
		return historicalAgentRun{}, false
	}
	return best, true
}

func readHistoricalRunResult(reportPath string) (Result, error) {
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		return Result{}, err
	}

	var result Result
	if err := json.Unmarshal(reportBytes, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func isUsableFeedbackResult(result Result) bool {
	if strings.TrimSpace(result.Status) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(result.NextAction), "retry") {
		return false
	}
	switch strings.TrimSpace(result.FailureFingerprint) {
	case ErrorCodeMalformedOutput, ErrorCodeRunnerExecution, ErrorCodeRunnerTimeout:
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(result.Status), "timeout")
}

func feedbackRefsFromHistoricalRun(run historicalAgentRun) FeedbackRefs {
	return FeedbackRefs{
		Attempt:            run.Attempt,
		Status:             run.Result.Status,
		NextAction:         run.Result.NextAction,
		FailureFingerprint: run.Result.FailureFingerprint,
		Summary:            run.Result.Summary,
		Findings:           append([]Finding(nil), run.Result.Findings...),
	}
}

func developerRefsFromHistoricalRun(run historicalAgentRun, changedFiles []string) DeveloperRefs {
	return DeveloperRefs{
		Attempt:      run.Attempt,
		Status:       run.Result.Status,
		NextAction:   run.Result.NextAction,
		Summary:      run.Result.Summary,
		ChangedFiles: append([]string(nil), changedFiles...),
	}
}

func listWorktreeChangedFiles(workdir string) []string {
	cmd := exec.Command("git", "status", "--short", "--untracked-files=all")
	cmd.Dir = workdir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	seen := map[string]struct{}{}
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		path := parseGitStatusPath(scanner.Text())
		if path == "" || shouldIgnoreChangedFile(path) {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func parseGitStatusPath(line string) string {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if len(line) >= 3 {
		line = line[3:]
	}
	if idx := strings.LastIndex(line, " -> "); idx >= 0 {
		line = line[idx+4:]
	}
	return filepath.ToSlash(strings.TrimSpace(line))
}

func shouldIgnoreChangedFile(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return path == "" || strings.HasPrefix(path, ".toolhub/") || strings.HasPrefix(path, ".git/")
}

func parseAttemptDirName(name string) (int, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(name), "attempt-")
	if value == "" || value == name {
		return 0, false
	}

	attempt, err := strconv.Atoi(value)
	if err != nil || attempt <= 0 {
		return 0, false
	}
	return attempt, true
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

	keys := []string{"TMPDIR", "TMP", "TEMP", "GOTMPDIR", "GOCACHE", "GOMODCACHE", "XDG_CACHE_HOME"}
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
	xdgCacheHome := filepath.Join(baseDir, ".cache")
	for _, dir := range []string{tmpDir, goBuildDir, goCacheDir, xdgCacheHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	goModCacheDir, err := resolveGoModCacheDir(workdir, baseDir)
	if err != nil {
		return nil, err
	}

	env = upsertEnv(env, "TMPDIR", tmpDir)
	env = upsertEnv(env, "TMP", tmpDir)
	env = upsertEnv(env, "TEMP", tmpDir)
	env = upsertEnv(env, "GOTMPDIR", goBuildDir)
	env = upsertEnv(env, "GOCACHE", goCacheDir)
	env = upsertEnv(env, "GOMODCACHE", goModCacheDir)
	env = upsertEnv(env, "XDG_CACHE_HOME", xdgCacheHome)
	if isolatedCodexHome {
		homeDir := filepath.Join(baseDir, "home")
		if err := os.MkdirAll(homeDir, 0o755); err != nil {
			return nil, err
		}
		env = upsertEnv(env, "HOME", homeDir)
	}
	return env, nil
}

func resolveGoModCacheDir(workdir string, runtimeDir string) (string, error) {
	primary := filepath.Join(runtimeDir, "go-mod-cache")
	if err := os.MkdirAll(primary, 0o755); err != nil {
		return "", err
	}

	hasEntries, err := dirHasEntries(primary)
	if err != nil {
		return "", err
	}
	if hasEntries {
		return primary, nil
	}

	legacyCandidates := []string{
		filepath.Join(runtimeDir, "tmp", "gomodcache"),
		filepath.Join(workdir, ".toolhub", ".modcache"),
	}
	for _, candidate := range legacyCandidates {
		hasEntries, err := dirHasEntries(candidate)
		if err != nil {
			return "", err
		}
		if hasEntries {
			return candidate, nil
		}
	}

	return primary, nil
}

func dirHasEntries(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(entries) > 0, nil
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
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		return newToolError(ErrorCodeMalformedOutput, "malformed_output: missing status", true)
	}
	if !isAllowedResultStatus(status) {
		return newToolError(ErrorCodeMalformedOutput, fmt.Sprintf("malformed_output: invalid status %q", payload.Status), true)
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

func getRequiredTrimmedString(value map[string]any, key string) (string, bool) {
	str, ok := getNullableString(value, key)
	if !ok {
		return "", false
	}
	str = strings.TrimSpace(str)
	if str == "" {
		return "", false
	}
	return str, true
}

func parseStringSlice(value any) ([]string, bool) {
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
	reviewerID, ok := getRequiredTrimmedString(entry, "reviewer_id")
	if !ok {
		return Finding{}, false
	}
	lens, ok := getRequiredTrimmedString(entry, "lens")
	if !ok {
		return Finding{}, false
	}
	lens, ok = NormalizeReviewerLens(lens)
	if !ok {
		return Finding{}, false
	}
	severity, ok := getRequiredTrimmedString(entry, "severity")
	if !ok {
		return Finding{}, false
	}
	severity, ok = NormalizeFindingSeverity(severity)
	if !ok {
		return Finding{}, false
	}
	confidence, ok := getRequiredTrimmedString(entry, "confidence")
	if !ok {
		return Finding{}, false
	}
	confidence, ok = NormalizeFindingConfidence(confidence)
	if !ok {
		return Finding{}, false
	}
	category, ok := getRequiredTrimmedString(entry, "category")
	if !ok {
		return Finding{}, false
	}
	fileRefs, ok := parseStringSlice(entry["file_refs"])
	if !ok {
		return Finding{}, false
	}
	summary, ok := getRequiredTrimmedString(entry, "summary")
	if !ok {
		return Finding{}, false
	}
	evidence, ok := getRequiredTrimmedString(entry, "evidence")
	if !ok {
		return Finding{}, false
	}
	fingerprint, ok := getRequiredTrimmedString(entry, "finding_fingerprint")
	if !ok {
		return Finding{}, false
	}
	suggestedAction, ok := getRequiredTrimmedString(entry, "suggested_action")
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
				fmt.Fprintf(out, "[progress] still running (%s)\n", formatHeartbeatElapsed(time.Since(startedAt)))
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

func formatHeartbeatElapsed(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return "<1m"
	}

	elapsed = elapsed.Truncate(time.Minute)
	hours := elapsed / time.Hour
	minutes := (elapsed % time.Hour) / time.Minute
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

func codexSandbox(agentType AgentType) string {
	if agentType == AgentReviewer {
		return "read-only"
	}
	return "workspace-write"
}
