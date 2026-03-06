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

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
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
		return runRunTask(ctx, args[1:], stdout)
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
	fmt.Fprintln(w, "  --runner                codex_exec | claude_print | opencode_run")
	fmt.Fprintln(w, "  --agent-type            developer | qa | reviewer")
	fmt.Fprintln(w, "  --attempt               Attempt number. Default 1.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for agent execution.")
	fmt.Fprintln(w, "  --output-root           Root directory for run artifacts.")
	fmt.Fprintln(w, "  --timeout               Timeout duration, e.g. 30m.")
	fmt.Fprintln(w, "  --model                 Optional runner model override.")
	fmt.Fprintln(w, "  --runner-agent          Required for opencode_run.")
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

func runRunTask(ctx context.Context, args []string, stdout io.Writer) error {
	var taskID string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		taskID = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("run-task", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var opts agentrun.RunOptions
	var timeout time.Duration
	fs.StringVar((*string)(&opts.Runner), "runner", string(agentrun.RunnerCodexExec), "runner id")
	fs.StringVar((*string)(&opts.AgentType), "agent-type", string(agentrun.AgentDeveloper), "agent type")
	fs.IntVar(&opts.Attempt, "attempt", 1, "attempt number")
	fs.StringVar(&opts.PlanFile, "plan-file", "plan/SPRINTS-V1.md", "path to Sprint plan")
	fs.StringVar(&opts.TasksDir, "tasks-dir", "plan/tasks", "path to task brief directory")
	fs.StringVar(&opts.WorkDir, "workdir", ".", "repository worktree for agent execution")
	fs.StringVar(&opts.OutputRoot, "output-root", ".toolhub/runs", "root directory for run artifacts")
	fs.DurationVar(&timeout, "timeout", 30*time.Minute, "runner timeout")
	fs.StringVar(&opts.Model, "model", "", "optional model override")
	fs.StringVar(&opts.OpencodeAgent, "runner-agent", "", "runner-specific agent name for opencode_run")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return nil
		}
		return err
	}

	if taskID == "" {
		if fs.NArg() != 1 {
			return errors.New("run-task requires exactly one task id argument")
		}
		taskID = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("run-task accepts flags only after the task id")
	}
	opts.TaskID = taskID
	opts.Timeout = timeout

	absWorkDir, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	opts.WorkDir = absWorkDir

	executor := agentrun.NewExecutor(agentrun.ExecCommandRunner{})
	result, err := executor.RunTask(ctx, opts)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
