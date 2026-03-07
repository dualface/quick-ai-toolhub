package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchBranchExistsAndRevParse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	remoteDir, seedDir, cloneDir := setupRemoteClone(t, env)

	client := New(Dependencies{})
	exists, err := client.BranchExists(ctx, BranchExistsRequest{
		WorkDir:  cloneDir,
		Branch:   "sprint/Sprint-03",
		Location: BranchLocationRemoteTracking,
		Remote:   "origin",
	})
	if err != nil {
		t.Fatalf("branch exists before fetch: %v", err)
	}
	if exists {
		t.Fatal("expected remote-tracking branch to be absent before fetch")
	}

	runGit(t, env, seedDir, "checkout", "-b", "sprint/Sprint-03", "main")
	writeFile(t, filepath.Join(seedDir, "sprint.txt"), "sprint branch\n")
	runGit(t, env, seedDir, "add", "sprint.txt")
	runGit(t, env, seedDir, "commit", "-m", "add sprint branch")
	runGit(t, env, seedDir, "push", "--set-upstream", "origin", "sprint/Sprint-03")

	if err := client.Fetch(ctx, FetchRequest{
		WorkDir: cloneDir,
		Remote:  "origin",
		Prune:   true,
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	exists, err = client.BranchExists(ctx, BranchExistsRequest{
		WorkDir:  cloneDir,
		Branch:   "sprint/Sprint-03",
		Location: BranchLocationRemoteTracking,
		Remote:   "origin",
	})
	if err != nil {
		t.Fatalf("branch exists after fetch: %v", err)
	}
	if !exists {
		t.Fatal("expected remote-tracking branch after fetch")
	}

	gotSHA, err := client.RevParse(ctx, RevParseRequest{
		WorkDir:  cloneDir,
		Revision: "refs/remotes/origin/sprint/Sprint-03",
	})
	if err != nil {
		t.Fatalf("rev-parse remote branch: %v", err)
	}

	wantSHA := runGit(t, env, cloneDir, "rev-parse", "refs/remotes/origin/sprint/Sprint-03")
	if gotSHA != wantSHA {
		t.Fatalf("unexpected remote branch sha: got %q want %q", gotSHA, wantSHA)
	}

	if _, err := os.Stat(remoteDir); err != nil {
		t.Fatalf("expected remote repo to exist: %v", err)
	}
}

func TestCreateCheckoutAndPushBranch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	remoteDir, _, cloneDir := setupRemoteClone(t, env)

	client := New(Dependencies{})
	exists, err := client.BranchExists(ctx, BranchExistsRequest{
		WorkDir: cloneDir,
		Branch:  "sprint/Sprint-03",
	})
	if err != nil {
		t.Fatalf("branch exists before create: %v", err)
	}
	if exists {
		t.Fatal("expected local branch to be absent before create")
	}

	if err := client.CreateBranch(ctx, CreateBranchRequest{
		WorkDir:    cloneDir,
		Branch:     "sprint/Sprint-03",
		StartPoint: "origin/main",
	}); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	exists, err = client.BranchExists(ctx, BranchExistsRequest{
		WorkDir: cloneDir,
		Branch:  "sprint/Sprint-03",
	})
	if err != nil {
		t.Fatalf("branch exists after create: %v", err)
	}
	if !exists {
		t.Fatal("expected local branch after create")
	}

	if err := client.Checkout(ctx, CheckoutRequest{
		WorkDir: cloneDir,
		Branch:  "sprint/Sprint-03",
	}); err != nil {
		t.Fatalf("checkout branch: %v", err)
	}

	currentBranch, err := client.RevParse(ctx, RevParseRequest{
		WorkDir:   cloneDir,
		Revision:  "HEAD",
		AbbrevRef: true,
	})
	if err != nil {
		t.Fatalf("rev-parse current branch: %v", err)
	}
	if currentBranch != "sprint/Sprint-03" {
		t.Fatalf("unexpected current branch: %q", currentBranch)
	}

	writeFile(t, filepath.Join(cloneDir, "push.txt"), "push branch\n")
	runGit(t, env, cloneDir, "add", "push.txt")
	runGit(t, env, cloneDir, "commit", "-m", "push branch")

	headSHA, err := client.RevParse(ctx, RevParseRequest{
		WorkDir:  cloneDir,
		Revision: "HEAD",
	})
	if err != nil {
		t.Fatalf("rev-parse head: %v", err)
	}

	if err := client.Push(ctx, PushRequest{
		WorkDir:     cloneDir,
		Remote:      "origin",
		Refspecs:    []string{"sprint/Sprint-03"},
		SetUpstream: true,
	}); err != nil {
		t.Fatalf("push branch: %v", err)
	}

	remoteSHA := runGit(t, env, remoteDir, "rev-parse", "refs/heads/sprint/Sprint-03")
	if headSHA != remoteSHA {
		t.Fatalf("unexpected remote sha: got %q want %q", remoteSHA, headSHA)
	}
}

func TestMergeBaseReturnsCommonAncestor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, _, cloneDir := setupRemoteClone(t, env)

	client := New(Dependencies{})
	baseSHA := runGit(t, env, cloneDir, "rev-parse", "main")

	if err := client.CreateBranch(ctx, CreateBranchRequest{
		WorkDir:    cloneDir,
		Branch:     "task/Sprint-03/Task-03",
		StartPoint: "main",
	}); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := client.Checkout(ctx, CheckoutRequest{
		WorkDir: cloneDir,
		Branch:  "task/Sprint-03/Task-03",
	}); err != nil {
		t.Fatalf("checkout branch: %v", err)
	}

	writeFile(t, filepath.Join(cloneDir, "task.txt"), "task branch change\n")
	runGit(t, env, cloneDir, "add", "task.txt")
	runGit(t, env, cloneDir, "commit", "-m", "task change")

	mergeBase, err := client.MergeBase(ctx, MergeBaseRequest{
		WorkDir:       cloneDir,
		LeftRevision:  "task/Sprint-03/Task-03",
		RightRevision: "main",
	})
	if err != nil {
		t.Fatalf("merge-base: %v", err)
	}
	if mergeBase != baseSHA {
		t.Fatalf("unexpected merge-base sha: got %q want %q", mergeBase, baseSHA)
	}
}

func TestAddListAndRemoveWorktree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newGitEnv(t)
	_, _, cloneDir := setupRemoteClone(t, env)

	client := New(Dependencies{})
	worktreePath := filepath.Join(t.TempDir(), "task-worktree")

	if err := client.AddWorktree(ctx, AddWorktreeRequest{
		WorkDir:      cloneDir,
		Path:         worktreePath,
		Branch:       "task/Sprint-03/Task-02",
		StartPoint:   "main",
		CreateBranch: true,
	}); err != nil {
		t.Fatalf("add worktree: %v", err)
	}

	items, err := client.ListWorktrees(ctx, ListWorktreesRequest{WorkDir: cloneDir})
	if err != nil {
		t.Fatalf("list worktrees after add: %v", err)
	}

	var found bool
	for _, item := range items {
		if item.Path != worktreePath {
			continue
		}
		found = true
		if item.Branch != "task/Sprint-03/Task-02" {
			t.Fatalf("unexpected worktree branch: %q", item.Branch)
		}
		if item.Detached {
			t.Fatal("expected branch worktree, got detached")
		}
	}
	if !found {
		t.Fatalf("worktree %q not found in list: %#v", worktreePath, items)
	}

	currentBranch, err := client.RevParse(ctx, RevParseRequest{
		WorkDir:   worktreePath,
		Revision:  "HEAD",
		AbbrevRef: true,
	})
	if err != nil {
		t.Fatalf("rev-parse worktree branch: %v", err)
	}
	if currentBranch != "task/Sprint-03/Task-02" {
		t.Fatalf("unexpected worktree current branch: %q", currentBranch)
	}

	if err := client.RemoveWorktree(ctx, RemoveWorktreeRequest{
		WorkDir: cloneDir,
		Path:    worktreePath,
	}); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	items, err = client.ListWorktrees(ctx, ListWorktreesRequest{WorkDir: cloneDir})
	if err != nil {
		t.Fatalf("list worktrees after remove: %v", err)
	}
	for _, item := range items {
		if item.Path == worktreePath {
			t.Fatalf("worktree %q still present after remove", worktreePath)
		}
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected removed worktree path to be absent, got err=%v", err)
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
