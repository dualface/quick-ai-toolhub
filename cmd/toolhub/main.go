package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "toolhub")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  toolhub sync-issues [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --apply                 Perform GitHub writes. Default is dry-run.")
	fmt.Fprintln(w, "  --plan-file             Path to the Sprint plan file.")
	fmt.Fprintln(w, "  --tasks-dir             Path to the task brief directory.")
	fmt.Fprintln(w, "  --manifest-file         Path to the generated manifest file.")
	fmt.Fprintln(w, "  --workdir               Repository worktree for gh commands.")
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
