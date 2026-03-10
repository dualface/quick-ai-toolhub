package worktreeprep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	toolgit "quick-ai-toolhub/internal/git"
)

func TestExecuteCreatesBranchesAndWorktree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, _, cloneDir := setupRemoteClone(t, env)

	service := newTestService(env)
	worktreeRoot := filepath.Join(t.TempDir(), "worktrees")
	req := Request{
		SprintID:     "Sprint-03",
		TaskID:       "Sprint-03/Task-03",
		SprintBranch: "sprint/Sprint-03",
		TaskBranch:   "task/Sprint-03/Task-03",
		WorktreeRoot: worktreeRoot,
	}

	response := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !response.OK {
		t.Fatalf("execute returned error: %+v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("expected response data")
	}
	if response.Data.Reused {
		t.Fatal("expected first execution to create a new environment")
	}

	wantPath := filepath.Join(worktreeRoot, "Sprint-03", "Task-03")
	if response.Data.WorktreePath != wantPath {
		t.Fatalf("unexpected worktree path: got %s want %s", response.Data.WorktreePath, wantPath)
	}
	if response.Data.TaskBranch != req.TaskBranch {
		t.Fatalf("unexpected task branch: %s", response.Data.TaskBranch)
	}
	if response.Data.BaseBranch != req.SprintBranch {
		t.Fatalf("unexpected base branch: %s", response.Data.BaseBranch)
	}

	client := newTestGitClient(env)
	assertLocalBranchExists(t, ctx, client, cloneDir, req.SprintBranch)
	assertLocalBranchExists(t, ctx, client, cloneDir, req.TaskBranch)
	assertRemoteBranchExists(t, ctx, client, cloneDir, req.SprintBranch)

	currentBranch, err := client.RevParse(ctx, toolgit.RevParseRequest{
		WorkDir:   response.Data.WorktreePath,
		Revision:  "HEAD",
		AbbrevRef: true,
	})
	if err != nil {
		t.Fatalf("rev-parse worktree branch: %v", err)
	}
	if currentBranch != req.TaskBranch {
		t.Fatalf("unexpected worktree branch: got %s want %s", currentBranch, req.TaskBranch)
	}

	mainSHA := runGit(t, env, cloneDir, "rev-parse", "main")
	if response.Data.BaseCommitSHA != mainSHA {
		t.Fatalf("unexpected base commit sha: got %s want %s", response.Data.BaseCommitSHA, mainSHA)
	}
}

func TestExecuteReusesExistingEnvironmentAndRefreshesSprintBranch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, seedDir, cloneDir := setupRemoteClone(t, env)

	service := newTestService(env)
	req := Request{
		SprintID:     "Sprint-03",
		TaskID:       "Sprint-03/Task-03",
		SprintBranch: "sprint/Sprint-03",
		TaskBranch:   "task/Sprint-03/Task-03",
		WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	}

	first := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !first.OK || first.Data == nil {
		t.Fatalf("first execute failed: %+v", first.Error)
	}
	originalBase := first.Data.BaseCommitSHA

	runGit(t, env, cloneDir, "push", "--set-upstream", "origin", req.SprintBranch)
	runGit(t, env, seedDir, "fetch", "origin")
	runGit(t, env, seedDir, "checkout", "-B", req.SprintBranch, "origin/"+req.SprintBranch)
	writeFile(t, filepath.Join(seedDir, "sprint.txt"), "latest sprint change\n")
	runGit(t, env, seedDir, "add", "sprint.txt")
	runGit(t, env, seedDir, "commit", "-m", "advance sprint branch")
	runGit(t, env, seedDir, "push", "origin", req.SprintBranch)
	latestSprintSHA := runGit(t, env, seedDir, "rev-parse", "HEAD")

	second := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !second.OK || second.Data == nil {
		t.Fatalf("second execute failed: %+v", second.Error)
	}
	if !second.Data.Reused {
		t.Fatal("expected second execution to reuse the existing environment")
	}
	if second.Data.WorktreePath != first.Data.WorktreePath {
		t.Fatalf("unexpected reused worktree path: got %s want %s", second.Data.WorktreePath, first.Data.WorktreePath)
	}
	if second.Data.BaseCommitSHA != originalBase {
		t.Fatalf("expected stable base commit sha: got %s want %s", second.Data.BaseCommitSHA, originalBase)
	}

	localSprintSHA := runGit(t, env, cloneDir, "rev-parse", req.SprintBranch)
	if localSprintSHA != latestSprintSHA {
		t.Fatalf("expected local sprint branch to refresh: got %s want %s", localSprintSHA, latestSprintSHA)
	}
}

func TestExecuteRejectsLocalSprintBranchThatCannotFastForward(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, _, cloneDir := setupRemoteClone(t, env)

	service := newTestService(env)
	req := Request{
		SprintID:     "Sprint-03",
		TaskID:       "Sprint-03/Task-03",
		SprintBranch: "sprint/Sprint-03",
		TaskBranch:   "task/Sprint-03/Task-03",
		WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	}

	first := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !first.OK || first.Data == nil {
		t.Fatalf("first execute failed: %+v", first.Error)
	}

	runGit(t, env, cloneDir, "push", "--set-upstream", "origin", req.SprintBranch)
	runGit(t, env, cloneDir, "checkout", req.SprintBranch)
	writeFile(t, filepath.Join(cloneDir, "local-only.txt"), "local sprint change\n")
	runGit(t, env, cloneDir, "add", "local-only.txt")
	runGit(t, env, cloneDir, "commit", "-m", "local sprint change")
	runGit(t, env, cloneDir, "checkout", "main")

	second := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if second.OK {
		t.Fatal("expected second execute to fail with a sprint branch conflict")
	}
	if second.Error == nil {
		t.Fatal("expected conflict error")
	}
	if second.Error.Code != ErrorCodeConflict {
		t.Fatalf("unexpected error code: got %s want %s", second.Error.Code, ErrorCodeConflict)
	}
	if !strings.Contains(second.Error.Message, "cannot be fast-forwarded") {
		t.Fatalf("unexpected error message: %s", second.Error.Message)
	}
}

func TestExecuteRejectsUpdatingSprintBranchCheckedOutInAnotherWorktree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, seedDir, cloneDir := setupRemoteClone(t, env)

	service := newTestService(env)
	req := Request{
		SprintID:     "Sprint-03",
		TaskID:       "Sprint-03/Task-03",
		SprintBranch: "sprint/Sprint-03",
		TaskBranch:   "task/Sprint-03/Task-03",
		WorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	}

	first := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !first.OK || first.Data == nil {
		t.Fatalf("first execute failed: %+v", first.Error)
	}

	runGit(t, env, cloneDir, "push", "--set-upstream", "origin", req.SprintBranch)

	sprintWorktree := filepath.Join(t.TempDir(), "sprint-worktree")
	runGit(t, env, cloneDir, "worktree", "add", sprintWorktree, req.SprintBranch)

	runGit(t, env, seedDir, "fetch", "origin")
	runGit(t, env, seedDir, "checkout", "-B", req.SprintBranch, "origin/"+req.SprintBranch)
	writeFile(t, filepath.Join(seedDir, "sprint.txt"), "latest sprint change\n")
	runGit(t, env, seedDir, "add", "sprint.txt")
	runGit(t, env, seedDir, "commit", "-m", "advance sprint branch")
	runGit(t, env, seedDir, "push", "origin", req.SprintBranch)

	second := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if second.OK {
		t.Fatal("expected second execute to fail when sprint branch is checked out elsewhere")
	}
	if second.Error == nil {
		t.Fatal("expected conflict error")
	}
	if second.Error.Code != ErrorCodeConflict {
		t.Fatalf("unexpected error code: got %s want %s", second.Error.Code, ErrorCodeConflict)
	}
	if !strings.Contains(second.Error.Message, sprintWorktree) {
		t.Fatalf("expected error to mention checked-out worktree %s, got %s", sprintWorktree, second.Error.Message)
	}
}

func TestExecuteReusesExistingTaskWorktreeEvenIfRequestedRootChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, _, cloneDir := setupRemoteClone(t, env)

	service := newTestService(env)
	req := Request{
		SprintID:     "Sprint-03",
		TaskID:       "Sprint-03/Task-03",
		SprintBranch: "sprint/Sprint-03",
		TaskBranch:   "task/Sprint-03/Task-03",
		WorktreeRoot: filepath.Join(t.TempDir(), "root-a"),
	}

	first := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !first.OK || first.Data == nil {
		t.Fatalf("first execute failed: %+v", first.Error)
	}

	req.WorktreeRoot = filepath.Join(t.TempDir(), "root-b")
	second := service.Execute(ctx, req, ExecuteOptions{
		WorkDir:       cloneDir,
		DefaultBranch: "main",
	})
	if !second.OK || second.Data == nil {
		t.Fatalf("second execute failed: %+v", second.Error)
	}
	if !second.Data.Reused {
		t.Fatal("expected existing task worktree to be reused")
	}
	if second.Data.WorktreePath != first.Data.WorktreePath {
		t.Fatalf("expected original worktree path to win: got %s want %s", second.Data.WorktreePath, first.Data.WorktreePath)
	}
}

func newTestService(env []string) *Service {
	return New(Dependencies{
		Git: newTestGitClient(env),
	})
}

func newTestGitClient(env []string) *toolgit.Client {
	return toolgit.New(toolgit.Dependencies{
		Runner: envRunner{env: env},
	})
}

type envRunner struct {
	env []string
}

func (r envRunner) Run(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workdir
	cmd.Env = r.env
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return output, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, message)
	}

	return output, nil
}

func assertLocalBranchExists(t *testing.T, ctx context.Context, client *toolgit.Client, workdir, branch string) {
	t.Helper()

	exists, err := client.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir: workdir,
		Branch:  branch,
	})
	if err != nil {
		t.Fatalf("branch exists %s: %v", branch, err)
	}
	if !exists {
		t.Fatalf("expected local branch %s to exist", branch)
	}
}

func assertRemoteBranchExists(t *testing.T, ctx context.Context, client *toolgit.Client, workdir, branch string) {
	t.Helper()

	exists, err := client.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir:  workdir,
		Branch:   branch,
		Location: toolgit.BranchLocationRemoteTracking,
		Remote:   "origin",
	})
	if err != nil {
		t.Fatalf("remote branch exists %s: %v", branch, err)
	}
	if !exists {
		t.Fatalf("expected remote branch %s to exist", branch)
	}
}

func setupRemoteClone(t *testing.T, env []string) (string, string, string) {
	t.Helper()

	baseDir := t.TempDir()
	remoteDir := filepath.Join(baseDir, "remote.git")
	seedDir := filepath.Join(baseDir, "seed")
	cloneDir := filepath.Join(baseDir, "clone")

	runGit(t, env, baseDir, "init", "--bare", remoteDir)

	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatalf("mkdir seed repo: %v", err)
	}
	runGit(t, env, seedDir, "init")
	configureTestAuthor(t, env, seedDir)

	writeFile(t, filepath.Join(seedDir, "README.md"), "initial\n")
	runGit(t, env, seedDir, "add", "README.md")
	runGit(t, env, seedDir, "commit", "-m", "initial commit")
	runGit(t, env, seedDir, "branch", "-M", "main")
	runGit(t, env, seedDir, "remote", "add", "origin", remoteDir)
	runGit(t, env, seedDir, "push", "--set-upstream", "origin", "main")
	runGit(t, env, remoteDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runGit(t, env, baseDir, "clone", remoteDir, cloneDir)
	configureTestAuthor(t, env, cloneDir)

	return remoteDir, seedDir, cloneDir
}

func configureTestAuthor(t *testing.T, env []string, workdir string) {
	t.Helper()

	runGit(t, env, workdir, "config", "user.name", "Toolhub Test")
	runGit(t, env, workdir, "config", "user.email", "toolhub@example.com")
}

func newGitEnv(t *testing.T) []string {
	t.Helper()

	homeDir := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir test home: %v", err)
	}

	env := append([]string{}, os.Environ()...)
	env = append(env,
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+homeDir,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	return env
}

func runGit(t *testing.T, env []string, workdir string, args ...string) string {
	t.Helper()

	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}

	cmd := exec.Command(gitPath, args...)
	cmd.Dir = workdir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), workdir, err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
