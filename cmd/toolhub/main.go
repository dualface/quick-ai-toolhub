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
	"time"

	"quick-ai-toolhub/internal/agentrun"
	"quick-ai-toolhub/internal/issuesync"
)

type runTaskExecutor interface {
	RunTask(ctx context.Context, opts agentrun.RunOptions) (agentrun.Result, error)
}

var newRunTaskExecutor = func() runTaskExecutor {
	return agentrun.NewExecutor(agentrun.ExecCommandRunner{})
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
	case "sync-issues":
		return runSyncIssues(ctx, args[1:], stdout)
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
	fmt.Fprintln(w, "  toolhub sync-issues [flags]")
	fmt.Fprintln(w, "  toolhub run-task <task-id> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "sync-issues Flags:")
	fmt.Fprintln(w, "  --apply                 Perform GitHub writes. Default is dry-run.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --manifest-file         Path to the generated manifest file.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for gh commands.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run-task Flags:")
	fmt.Fprintln(w, "  --agent-type            developer | qa | reviewer")
	fmt.Fprintln(w, "  --attempt               Attempt number. Default 1.")
	fmt.Fprintln(w, "  --lens                  Optional execution lens.")
	fmt.Fprintln(w, "  --github-pr-number      Optional GitHub PR number in context.")
	fmt.Fprintln(w, "  --context-log           Optional upstream context log artifact.")
	fmt.Fprintln(w, "  --context-patch         Optional upstream context patch artifact.")
	fmt.Fprintln(w, "  --context-report        Optional upstream context report artifact.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for agent execution.")
	fmt.Fprintln(w, "  --output-root           Root directory for run artifacts.")
	fmt.Fprintln(w, "  --timeout               Timeout duration, e.g. 30m.")
	fmt.Fprintln(w, "  --model                 Optional runner model override.")
	fmt.Fprintln(w, "  --stream                Stream live agent output to stderr.")
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
	fs.StringVar((*string)(&opts.AgentType), "agent-type", string(agentrun.AgentDeveloper), "agent type")
	fs.IntVar(&opts.Attempt, "attempt", 1, "attempt number")
	fs.StringVar(&opts.Lens, "lens", "", "optional execution lens")
	fs.IntVar(&opts.ContextRefs.GitHubPRNumber, "github-pr-number", 0, "optional GitHub PR number")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Log, "context-log", "", "optional upstream context log artifact")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Patch, "context-patch", "", "optional upstream context patch artifact")
	fs.StringVar(&opts.ContextRefs.ArtifactRefs.Report, "context-report", "", "optional upstream context report artifact")
	fs.StringVar(&opts.PlanFile, "plan-file", "plan/SPRINTS-V1.md", "path to Sprint plan")
	fs.StringVar(&opts.TasksDir, "tasks-dir", "plan/tasks", "path to task brief directory")
	fs.StringVar(&opts.WorkDir, "workdir", ".", "repository worktree for agent execution")
	fs.StringVar(&opts.OutputRoot, "output-root", ".toolhub/runs", "root directory for run artifacts")
	fs.DurationVar(&timeout, "timeout", 30*time.Minute, "runner timeout")
	fs.StringVar(&opts.Model, "model", "", "optional model override")
	fs.BoolVar(&stream, "stream", false, "stream live agent output to stderr")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return writeErrorResponse(stdout, agentrun.AsToolError(err), 2)
	}

	if taskID == "" {
		if fs.NArg() != 1 {
			return writeErrorResponse(stdout, &agentrun.ToolError{
				Code:      agentrun.ErrorCodeInvalidRequest,
				Message:   "run-task requires exactly one task id argument",
				Retryable: false,
			}, 2)
		}
		taskID = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return writeErrorResponse(stdout, &agentrun.ToolError{
			Code:      agentrun.ErrorCodeInvalidRequest,
			Message:   "run-task accepts flags only after the task id",
			Retryable: false,
		}, 2)
	}
	opts.TaskID = taskID
	opts.Timeout = timeout
	if stream {
		opts.StreamOutput = stderr
	}

	absWorkDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return writeErrorResponse(stdout, agentrun.AsToolError(fmt.Errorf("resolve workdir: %w", err)), 1)
	}
	opts.WorkDir = absWorkDir
	opts.ContextRefs.WorktreePath = absWorkDir

	executor := newRunTaskExecutor()
	result, err := executor.RunTask(ctx, opts)
	if err != nil {
		return writeErrorResponse(stdout, agentrun.AsToolError(err), 1)
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(commandResponse{
		OK:   true,
		Data: &result,
	})
}

func writeErrorResponse(stdout io.Writer, toolErr *agentrun.ToolError, exitCode int) error {
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
