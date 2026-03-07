package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quick-ai-toolhub/internal/agentrun"
	"quick-ai-toolhub/internal/app"
	sharedconfig "quick-ai-toolhub/internal/config"
	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/githubsync"
	"quick-ai-toolhub/internal/issuesync"
	"quick-ai-toolhub/internal/logging"
	"quick-ai-toolhub/internal/store"
	"quick-ai-toolhub/internal/tasklist"
	"quick-ai-toolhub/internal/worktreeprep"
)

type runTaskExecutor interface {
	RunTask(ctx context.Context, opts agentrun.RunOptions) (agentrun.Result, error)
}

type prepareWorktreeExecutor interface {
	Execute(context.Context, worktreeprep.Request, worktreeprep.ExecuteOptions) worktreeprep.Response
}

var newRunTaskExecutor = func() runTaskExecutor {
	return agentrun.NewExecutor(agentrun.ExecCommandRunner{})
}

var newPrepareWorktreeExecutor = func() prepareWorktreeExecutor {
	return worktreeprep.New(worktreeprep.Dependencies{})
}

var runServeApplication = func(ctx context.Context, application *app.Application) error {
	return application.Serve(ctx)
}

type commandResponse struct {
	OK    bool                `json:"ok"`
	Data  *agentrun.Result    `json:"data,omitempty"`
	Error *agentrun.ToolError `json:"error,omitempty"`
}

type cliExitError struct {
	code int
}

func (e *cliExitError) Error() string { return "" }

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var exitErr *cliExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.code)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	case "serve":
		return runServe(ctx, args[1:], stdout)
	case "sync-issues":
		return runSyncIssues(ctx, args[1:], stdout)
	case "github-sync", "github-sync-tool":
		return runGitHubSync(ctx, args[1:], stdout)
	case "get-task-list", "get-task-list-tool":
		return runGetTaskList(ctx, args[1:], stdout)
	case "prepare-worktree", "prepare-worktree-tool":
		return runPrepareWorktree(ctx, args[1:], stdout)
	case "run-task":
		return runRunTask(ctx, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "toolhub")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  toolhub serve")
	fmt.Fprintln(w, "  toolhub sync-issues [flags]")
	fmt.Fprintln(w, "  toolhub github-sync-tool <op> [flags]")
	fmt.Fprintln(w, "  toolhub get-task-list-tool [flags]")
	fmt.Fprintln(w, "  toolhub prepare-worktree-tool [flags]")
	fmt.Fprintln(w, "  toolhub run-task <task-id> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  serve                 Bootstrap the single-process application skeleton.")
	fmt.Fprintln(w, "  github-sync-tool      Reconcile GitHub issues / PRs / CI into SQLite projections.")
	fmt.Fprintln(w, "  get-task-list-tool    Read projected Sprint/Task state and report scheduling blockers.")
	fmt.Fprintln(w, "  prepare-worktree-tool Create or reuse the Sprint/task branches and task worktree.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "sync-issues Flags:")
	fmt.Fprintln(w, "  --apply                 Perform GitHub writes. Default is dry-run.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --manifest-file         Path to the generated manifest file.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for gh commands.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "github-sync-tool Flags:")
	fmt.Fprintln(w, "  <op>                    full_reconcile | ingest_webhook | reconcile_issue | reconcile_pull_request | reconcile_ci_run")
	fmt.Fprintln(w, "  (or)                    Read a JSON request from stdin when <op> is omitted.")
	fmt.Fprintln(w, "  --reason                full_reconcile reason: startup | periodic | manual")
	fmt.Fprintln(w, "  --config-file           Path to the repository config file. Defaults to CONFIG_FILE or config/config.yaml.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for gh commands and config discovery.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "get-task-list-tool Flags:")
	fmt.Fprintln(w, "  --refresh-mode          full | targeted")
	fmt.Fprintln(w, "  --sprint-id             Required when refresh-mode=targeted.")
	fmt.Fprintln(w, "  --config-file           Path to the repository config file. Defaults to CONFIG_FILE or config/config.yaml.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for config discovery.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "prepare-worktree-tool Flags:")
	fmt.Fprintln(w, "  --sprint-id             Required sprint id.")
	fmt.Fprintln(w, "  --task-id               Required task id.")
	fmt.Fprintln(w, "  --sprint-branch         Optional; defaults to sprint/<sprint-id>.")
	fmt.Fprintln(w, "  --task-branch           Optional; defaults to task/<task-id>.")
	fmt.Fprintln(w, "  --worktree-root         Optional root for task worktrees.")
	fmt.Fprintln(w, "  --remote                Git remote name. Defaults to origin.")
	fmt.Fprintln(w, "  --config-file           Path to the repository config file. Defaults to CONFIG_FILE or config/config.yaml.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for config discovery and git operations.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run-task Flags:")
	fmt.Fprintln(w, "  --agent-type            developer | qa | reviewer")
	fmt.Fprintln(w, "  --attempt               Attempt number. Default 1.")
	fmt.Fprintln(w, "  --lens                  Optional execution lens.")
	fmt.Fprintln(w, "  --github-pr-number      Optional GitHub PR number in context.")
	fmt.Fprintln(w, "  --context-log           Optional upstream context log artifact.")
	fmt.Fprintln(w, "  --context-patch         Optional upstream context patch artifact.")
	fmt.Fprintln(w, "  --context-report        Optional upstream context report artifact.")
	fmt.Fprintln(w, "  --config-file           Path to the repository config file. Defaults to CONFIG_FILE or config/config.yaml.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for agent execution.")
	fmt.Fprintln(w, "  --output-root           Root directory for run artifacts.")
	fmt.Fprintln(w, "  --timeout               Timeout duration, e.g. 30m.")
	fmt.Fprintln(w, "  --model                 Optional runner model override.")
	fmt.Fprintln(w, "  --yolo                  Bypass approvals and sandbox for codex.")
	fmt.Fprintln(w, "  --isolated-codex-home   Developer/qa only: redirect codex HOME into .toolhub/runtime/home.")
	fmt.Fprintln(w, "  --stream                Stream live agent output to stderr.")
	fmt.Fprintln(w, "  --no-progress           Disable low-noise progress updates to stderr.")
}

func runServe(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("serve does not accept positional arguments")
	}

	cfg, err := sharedconfig.Load(".", "")
	if err != nil {
		return err
	}

	logger := logging.InitDefault(stdout)
	application := app.New(app.Options{
		Logger: logger,
		Config: &cfg,
	})
	return runServeApplication(ctx, application)
}

func runSyncIssues(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sync-issues", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts issuesync.Options
	fs.BoolVar(&opts.Apply, "apply", false, "perform GitHub writes")
	fs.StringVar(&opts.PlanFile, "plan-file", "plan/SPRINTS-V1.md", "path to Sprint plan")
	fs.StringVar(&opts.TasksDir, "tasks-dir", "plan/tasks", "path to task brief directory")
	fs.StringVar(&opts.ManifestFile, "manifest-file", ".toolhub/issues-manifest.json", "path to manifest file")
	fs.StringVar(&opts.WorkDir, "workdir", ".", "repository worktree for gh commands")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}

	absWorkDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = absWorkDir

	parser := issuesync.Parser{}
	planData, err := parser.Load(opts.PlanFile, opts.TasksDir)
	if err != nil {
		return err
	}

	manifest, err := issuesync.LoadManifest(opts.ManifestFile)
	if err != nil {
		return err
	}

	syncer := issuesync.Syncer{
		Client:   issuesync.NewGitHubCLI(opts.WorkDir, issuesync.ExecRunner{}),
		Manifest: manifest,
		Writer:   stdout,
		Options:  opts,
	}

	if err := syncer.Sync(ctx, planData); err != nil {
		return err
	}

	if opts.Apply {
		if err := manifest.Save(opts.ManifestFile); err != nil {
			return err
		}
	}

	return nil
}

func runGitHubSync(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("github-sync-tool", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var configFile string
	var workdir string
	var reason string
	fs.StringVar(&configFile, "config-file", "", "path to repository config")
	fs.StringVar(&workdir, "workdir", ".", "repository worktree for gh commands and config discovery")
	fs.StringVar(&reason, "reason", "manual", "full_reconcile reason")

	if err := fs.Parse(args); err != nil {
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}

	var (
		request githubsync.Request
		err     error
	)
	switch fs.NArg() {
	case 0:
		request, err = readGitHubSyncRequest(os.Stdin)
		if err != nil {
			return writeGitHubSyncResponse(stdout, githubsync.Response{
				OK: false,
				Error: &githubsync.ToolError{
					Code:      githubsync.ErrorCodeInvalid,
					Message:   err.Error(),
					Retryable: false,
				},
			})
		}
	case 1:
		request = githubsync.Request{
			Op: githubsync.Operation(fs.Arg(0)),
		}
	default:
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeInvalid,
				Message:   "github-sync-tool accepts a single op argument",
				Retryable: false,
			},
		})
	}

	if request.Op == githubsync.OpFullReconcile {
		payload, err := json.Marshal(githubsync.FullReconcilePayload{Reason: reason})
		if err != nil {
			return writeGitHubSyncResponse(stdout, githubsync.Response{
				OK: false,
				Error: &githubsync.ToolError{
					Code:      githubsync.ErrorCodeInternal,
					Message:   fmt.Sprintf("marshal full_reconcile payload: %v", err),
					Retryable: false,
				},
			})
		}
		if len(request.Payload) == 0 || string(request.Payload) == "{}" {
			request.Payload = payload
		}
	} else {
		if len(request.Payload) == 0 {
			request.Payload = json.RawMessage(`{}`)
		}
	}

	absWorkDir, err := filepath.Abs(workdir)
	if err != nil {
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeInvalid,
				Message:   fmt.Sprintf("resolve workdir: %v", err),
				Retryable: false,
			},
		})
	}

	cfg, err := sharedconfig.Load(absWorkDir, configFile)
	if err != nil {
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}

	logger := logging.NewJSON(io.Discard)
	storeService := store.New(store.Dependencies{Logger: logger})
	defer func() {
		_ = storeService.Close()
	}()
	if err := storeService.Open(ctx, store.OpenOptions{
		ConfigPath:   cfg.Path,
		DatabasePath: cfg.Database.Path,
	}); err != nil {
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeProjection,
				Message:   fmt.Sprintf("open store: %v", err),
				Retryable: false,
			},
		})
	}

	baseStore, err := storeService.BaseStore()
	if err != nil {
		return writeGitHubSyncResponse(stdout, githubsync.Response{
			OK: false,
			Error: &githubsync.ToolError{
				Code:      githubsync.ErrorCodeProjection,
				Message:   fmt.Sprintf("open base store: %v", err),
				Retryable: false,
			},
		})
	}

	response := githubsync.New(githubsync.Dependencies{
		Logger: logger,
		GitHub: toolgithub.New(toolgithub.Dependencies{Logger: logger}),
		Store:  baseStore,
	}).Execute(ctx, request, githubsync.ExecuteOptions{
		WorkDir:       absWorkDir,
		Repo:          cfg.Repo.GitHubOwner + "/" + cfg.Repo.GitHubRepo,
		DefaultBranch: cfg.Repo.DefaultBranch,
	})

	return writeGitHubSyncResponse(stdout, response)
}

func runGetTaskList(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("get-task-list-tool", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var configFile string
	var workdir string
	var refreshMode string
	var sprintID string
	fs.StringVar(&configFile, "config-file", "", "path to repository config")
	fs.StringVar(&workdir, "workdir", ".", "repository worktree for config discovery")
	fs.StringVar(&refreshMode, "refresh-mode", string(tasklist.RefreshModeFull), "task list refresh mode")
	fs.StringVar(&sprintID, "sprint-id", "", "target sprint id")

	if err := fs.Parse(args); err != nil {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}
	if fs.NArg() != 0 {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInvalid,
				Message:   "get-task-list-tool does not accept positional arguments",
				Retryable: false,
			},
		})
	}

	absWorkDir, err := filepath.Abs(workdir)
	if err != nil {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInvalid,
				Message:   fmt.Sprintf("resolve workdir: %v", err),
				Retryable: false,
			},
		})
	}

	cfg, err := sharedconfig.Load(absWorkDir, configFile)
	if err != nil {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}

	logger := logging.NewJSON(io.Discard)
	storeService := store.New(store.Dependencies{Logger: logger})
	defer func() {
		_ = storeService.Close()
	}()
	if err := storeService.Open(ctx, store.OpenOptions{
		ConfigPath:   cfg.Path,
		DatabasePath: cfg.Database.Path,
	}); err != nil {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInternal,
				Message:   fmt.Sprintf("open store: %v", err),
				Retryable: false,
			},
		})
	}

	baseStore, err := storeService.BaseStore()
	if err != nil {
		return writeTaskListResponse(stdout, tasklist.Response{
			OK: false,
			Error: &tasklist.ToolError{
				Code:      tasklist.ErrorCodeInternal,
				Message:   fmt.Sprintf("open base store: %v", err),
				Retryable: false,
			},
		})
	}

	response := tasklist.New(tasklist.Dependencies{
		Logger: logger,
		Store:  baseStore,
	}).Execute(ctx, tasklist.Request{
		RefreshMode: tasklist.RefreshMode(refreshMode),
		SprintID:    sprintID,
	})

	return writeTaskListResponse(stdout, response)
}

func runPrepareWorktree(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("prepare-worktree-tool", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var configFile string
	var workdir string
	var remote string
	var request worktreeprep.Request
	fs.StringVar(&request.SprintID, "sprint-id", "", "sprint id")
	fs.StringVar(&request.TaskID, "task-id", "", "task id")
	fs.StringVar(&request.SprintBranch, "sprint-branch", "", "sprint branch")
	fs.StringVar(&request.TaskBranch, "task-branch", "", "task branch")
	fs.StringVar(&request.WorktreeRoot, "worktree-root", "", "task worktree root")
	fs.StringVar(&remote, "remote", "", "git remote name")
	fs.StringVar(&configFile, "config-file", "", "path to repository config")
	fs.StringVar(&workdir, "workdir", ".", "repository worktree for config discovery and git operations")

	if err := fs.Parse(args); err != nil {
		return writePrepareWorktreeResponse(stdout, worktreeprep.Response{
			OK: false,
			Error: &worktreeprep.ToolError{
				Code:      worktreeprep.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}
	if fs.NArg() != 0 {
		return writePrepareWorktreeResponse(stdout, worktreeprep.Response{
			OK: false,
			Error: &worktreeprep.ToolError{
				Code:      worktreeprep.ErrorCodeInvalid,
				Message:   "prepare-worktree-tool does not accept positional arguments",
				Retryable: false,
			},
		})
	}

	request.SprintID = strings.TrimSpace(request.SprintID)
	request.TaskID = strings.TrimSpace(request.TaskID)
	request.SprintBranch = strings.TrimSpace(request.SprintBranch)
	request.TaskBranch = strings.TrimSpace(request.TaskBranch)
	request.WorktreeRoot = strings.TrimSpace(request.WorktreeRoot)
	if request.SprintBranch == "" && request.SprintID != "" {
		request.SprintBranch = "sprint/" + request.SprintID
	}
	if request.TaskBranch == "" && request.TaskID != "" {
		request.TaskBranch = "task/" + request.TaskID
	}

	absWorkDir, err := filepath.Abs(workdir)
	if err != nil {
		return writePrepareWorktreeResponse(stdout, worktreeprep.Response{
			OK: false,
			Error: &worktreeprep.ToolError{
				Code:      worktreeprep.ErrorCodeInvalid,
				Message:   fmt.Sprintf("resolve workdir: %v", err),
				Retryable: false,
			},
		})
	}

	cfg, err := sharedconfig.Load(absWorkDir, configFile)
	if err != nil {
		return writePrepareWorktreeResponse(stdout, worktreeprep.Response{
			OK: false,
			Error: &worktreeprep.ToolError{
				Code:      worktreeprep.ErrorCodeInvalid,
				Message:   err.Error(),
				Retryable: false,
			},
		})
	}

	response := newPrepareWorktreeExecutor().Execute(ctx, request, worktreeprep.ExecuteOptions{
		WorkDir:       absWorkDir,
		DefaultBranch: cfg.Repo.DefaultBranch,
		Remote:        remote,
	})
	return writePrepareWorktreeResponse(stdout, response)
}

func readGitHubSyncRequest(r io.Reader) (githubsync.Request, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return githubsync.Request{}, fmt.Errorf("read github-sync-tool request: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return githubsync.Request{}, errors.New("github-sync-tool requires an op argument or a JSON request on stdin")
	}

	var request githubsync.Request
	if err := json.Unmarshal(data, &request); err != nil {
		return githubsync.Request{}, fmt.Errorf("decode github-sync-tool request: %w", err)
	}
	return request, nil
}

func runRunTask(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var taskID string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		taskID = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("run-task", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts agentrun.RunOptions
	var timeout time.Duration
	var stream bool
	var noProgress bool
	fs.StringVar((*string)(&opts.AgentType), "agent-type", string(agentrun.AgentDeveloper), "agent type")
	fs.IntVar(&opts.Attempt, "attempt", 1, "attempt number")
	fs.StringVar(&opts.Lens, "lens", "", "optional execution lens")
	fs.IntVar(&opts.ContextRefs.GitHubPRNumber, "github-pr-number", 0, "optional GitHub PR number")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Log, "context-log", "", "optional upstream context log artifact")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Patch, "context-patch", "", "optional upstream context patch artifact")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Report, "context-report", "", "optional upstream context report artifact")
	fs.StringVar(&opts.ConfigFile, "config-file", "", "path to repository config")
	fs.StringVar(&opts.PlanFile, "plan-file", "plan/SPRINTS-V1.md", "path to Sprint plan")
	fs.StringVar(&opts.TasksDir, "tasks-dir", "plan/tasks", "path to task brief directory")
	fs.StringVar(&opts.WorkDir, "workdir", ".", "repository worktree for agent execution")
	fs.StringVar(&opts.OutputRoot, "output-root", ".toolhub/runs", "root directory for run artifacts")
	fs.DurationVar(&timeout, "timeout", 30*time.Minute, "runner timeout")
	fs.StringVar(&opts.Model, "model", "", "optional model override")
	fs.BoolVar(&opts.Yolo, "yolo", false, "bypass approvals and sandbox for codex")
	fs.BoolVar(&opts.IsolatedCodexHome, "isolated-codex-home", false, "developer/qa only: redirect codex HOME into .toolhub/runtime/home")
	fs.BoolVar(&stream, "stream", false, "stream live agent output to stderr")
	fs.BoolVar(&noProgress, "no-progress", false, "disable low-noise progress updates to stderr")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return writeErrorResponse(stdout, &agentrun.ToolError{
			Code:      agentrun.ErrorCodeInvalidRequest,
			Message:   err.Error(),
			Retryable: false,
		}, 2, stream)
	}

	if taskID == "" {
		if fs.NArg() != 1 {
			return writeErrorResponse(stdout, &agentrun.ToolError{
				Code:      agentrun.ErrorCodeInvalidRequest,
				Message:   "run-task requires exactly one task id argument",
				Retryable: false,
			}, 2, stream)
		}
		taskID = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return writeErrorResponse(stdout, &agentrun.ToolError{
			Code:      agentrun.ErrorCodeInvalidRequest,
			Message:   "run-task accepts flags only after the task id",
			Retryable: false,
		}, 2, stream)
	}
	opts.TaskID = taskID
	opts.Timeout = timeout
	if stream {
		opts.StreamOutput = stderr
	} else if !noProgress {
		opts.ProgressOutput = stderr
	}

	absWorkDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return writeErrorResponse(stdout, agentrun.AsToolError(fmt.Errorf("resolve workdir: %w", err)), 1, stream)
	}
	opts.WorkDir = absWorkDir
	opts.ContextRefs.WorktreePath = absWorkDir

	executor := newRunTaskExecutor()
	result, err := executor.RunTask(ctx, opts)
	if err != nil {
		return writeErrorResponse(stdout, agentrun.AsToolError(err), 1, stream)
	}

	if stream {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(commandResponse{
			OK:   true,
			Data: &result,
		})
	}

	return writeHumanResult(stdout, taskID, result)
}

func writeGitHubSyncResponse(stdout io.Writer, response githubsync.Response) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return err
	}
	if response.OK {
		return nil
	}

	exitCode := 1
	if response.Error != nil && response.Error.Code == githubsync.ErrorCodeInvalid {
		exitCode = 2
	}
	return &cliExitError{code: exitCode}
}

func writeTaskListResponse(stdout io.Writer, response tasklist.Response) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return err
	}
	if response.OK {
		return nil
	}

	exitCode := 1
	if response.Error != nil && response.Error.Code == tasklist.ErrorCodeInvalid {
		exitCode = 2
	}
	return &cliExitError{code: exitCode}
}

func writePrepareWorktreeResponse(stdout io.Writer, response worktreeprep.Response) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return err
	}
	if response.OK {
		return nil
	}

	exitCode := 1
	if response.Error != nil && response.Error.Code == worktreeprep.ErrorCodeInvalid {
		exitCode = 2
	}
	return &cliExitError{code: exitCode}
}

func writeErrorResponse(stdout io.Writer, toolErr *agentrun.ToolError, exitCode int, jsonOutput bool) error {
	if !jsonOutput {
		if err := writeHumanError(stdout, toolErr); err != nil {
			return err
		}
		return &cliExitError{code: exitCode}
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(commandResponse{
		OK:    false,
		Error: toolErr,
	}); err != nil {
		return err
	}
	return &cliExitError{code: exitCode}
}

func writeHumanResult(w io.Writer, taskID string, result agentrun.Result) error {
	fmt.Fprintf(w, "Task: %s\n", taskID)
	fmt.Fprintf(w, "Runner: %s\n", result.Runner)
	fmt.Fprintf(w, "Status: %s\n", result.Status)
	fmt.Fprintf(w, "Next: %s\n", result.NextAction)
	if result.FailureFingerprint != "" {
		fmt.Fprintf(w, "Failure Fingerprint: %s\n", result.FailureFingerprint)
	}
	if result.SessionID != "" {
		fmt.Fprintf(w, "Session: %s\n", result.SessionID)
	}
	if strings.TrimSpace(result.Summary) != "" {
		fmt.Fprintf(w, "\nSummary:\n%s\n", result.Summary)
	}
	if refs := formatArtifactRefs(result.ArtifactRefs); refs != "" {
		fmt.Fprintf(w, "\nArtifacts:\n%s", refs)
	}
	if len(result.Findings) > 0 {
		fmt.Fprintf(w, "\nFindings (%d):\n", len(result.Findings))
		for i, finding := range result.Findings {
			severity := finding.Severity
			if severity == "" {
				severity = "unknown"
			}
			fmt.Fprintf(w, "%d. [%s] %s\n", i+1, severity, finding.Summary)
			if finding.SuggestedAction != "" {
				fmt.Fprintf(w, "   Action: %s\n", finding.SuggestedAction)
			}
		}
	}
	return nil
}

func writeHumanError(w io.Writer, toolErr *agentrun.ToolError) error {
	if toolErr == nil {
		return nil
	}
	fmt.Fprintf(w, "Error: %s\n", toolErr.Code)
	fmt.Fprintf(w, "Message: %s\n", toolErr.Message)
	fmt.Fprintf(w, "Retryable: %t\n", toolErr.Retryable)
	return nil
}

func formatArtifactRefs(refs agentrun.ArtifactRefs) string {
	var b strings.Builder
	if refs.Log != "" {
		fmt.Fprintf(&b, "- log: %s\n", refs.Log)
	}
	if refs.Worktree != "" {
		fmt.Fprintf(&b, "- worktree: %s\n", refs.Worktree)
	}
	if refs.Patch != "" {
		fmt.Fprintf(&b, "- patch: %s\n", refs.Patch)
	}
	if refs.Report != "" {
		fmt.Fprintf(&b, "- report: %s\n", refs.Report)
	}
	return b.String()
}
