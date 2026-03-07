package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (c *Client) Fetch(ctx context.Context, req FetchRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}

	args := []string{"git", "fetch"}
	if req.Prune {
		args = append(args, "--prune")
	}
	args = append(args, normalizeRemote(req.Remote))
	args = append(args, req.Refspecs...)

	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) BranchExists(ctx context.Context, req BranchExistsRequest) (bool, error) {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return false, err
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return false, errors.New("branch is required")
	}

	ref, err := resolveBranchRef(branch, req.Location, req.Remote)
	if err != nil {
		return false, err
	}

	out, err := c.output(ctx, req.WorkDir, "git", "for-each-ref", "--format=%(refname)", ref)
	if err != nil {
		return false, err
	}

	return strings.TrimSpace(out) != "", nil
}

func (c *Client) CreateBranch(ctx context.Context, req CreateBranchRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return errors.New("branch is required")
	}

	args := []string{"git", "branch"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, branch)
	if startPoint := strings.TrimSpace(req.StartPoint); startPoint != "" {
		args = append(args, startPoint)
	}

	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) Checkout(ctx context.Context, req CheckoutRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return errors.New("branch is required")
	}

	args := []string{"git", "checkout"}
	if req.Force {
		args = append(args, "--force")
	}
	if req.Create {
		args = append(args, "-b", branch)
		if startPoint := strings.TrimSpace(req.StartPoint); startPoint != "" {
			args = append(args, startPoint)
		}
	} else {
		if strings.TrimSpace(req.StartPoint) != "" {
			return errors.New("start_point requires create=true")
		}
		args = append(args, branch)
	}

	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) AddWorktree(ctx context.Context, req AddWorktreeRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return errors.New("path is required")
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		return errors.New("branch is required")
	}

	args := []string{"git", "worktree", "add"}
	if req.Force {
		args = append(args, "--force")
	}
	if req.CreateBranch {
		args = append(args, "-b", branch, path)
		if startPoint := strings.TrimSpace(req.StartPoint); startPoint != "" {
			args = append(args, startPoint)
		}
		return c.run(ctx, req.WorkDir, args...)
	}
	if strings.TrimSpace(req.StartPoint) != "" {
		return errors.New("start_point requires create_branch=true")
	}

	args = append(args, path, branch)
	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) ListWorktrees(ctx context.Context, req ListWorktreesRequest) ([]Worktree, error) {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return nil, err
	}

	out, err := c.output(ctx, req.WorkDir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	items, err := parseWorktrees(out)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) RemoveWorktree(ctx context.Context, req RemoveWorktreeRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return errors.New("path is required")
	}

	args := []string{"git", "worktree", "remove"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, path)

	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) RevParse(ctx context.Context, req RevParseRequest) (string, error) {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return "", err
	}
	revision := strings.TrimSpace(req.Revision)
	if revision == "" {
		return "", errors.New("revision is required")
	}

	args := []string{"git", "rev-parse", "--verify"}
	if req.AbbrevRef {
		args = append(args, "--abbrev-ref")
	}
	args = append(args, revision)

	out, err := c.output(ctx, req.WorkDir, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) MergeBase(ctx context.Context, req MergeBaseRequest) (string, error) {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return "", err
	}
	left := strings.TrimSpace(req.LeftRevision)
	if left == "" {
		return "", errors.New("left_revision is required")
	}
	right := strings.TrimSpace(req.RightRevision)
	if right == "" {
		return "", errors.New("right_revision is required")
	}

	out, err := c.output(ctx, req.WorkDir, "git", "merge-base", left, right)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) Push(ctx context.Context, req PushRequest) error {
	if err := validateWorkDir(req.WorkDir); err != nil {
		return err
	}
	if len(req.Refspecs) == 0 {
		return errors.New("refspecs are required")
	}

	args := []string{"git", "push"}
	if req.SetUpstream {
		args = append(args, "--set-upstream")
	}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, normalizeRemote(req.Remote))
	args = append(args, req.Refspecs...)

	return c.run(ctx, req.WorkDir, args...)
}

func (c *Client) run(ctx context.Context, workdir string, args ...string) error {
	_, err := c.runner.Run(ctx, workdir, args...)
	return err
}

func (c *Client) output(ctx context.Context, workdir string, args ...string) (string, error) {
	out, err := c.runner.Run(ctx, workdir, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func validateWorkDir(workdir string) error {
	if strings.TrimSpace(workdir) == "" {
		return errors.New("workdir is required")
	}
	return nil
}

func normalizeRemote(remote string) string {
	if strings.TrimSpace(remote) == "" {
		return defaultRemoteName
	}
	return strings.TrimSpace(remote)
}

func resolveBranchRef(branch string, location BranchLocation, remote string) (string, error) {
	switch normalizeBranchLocation(location) {
	case BranchLocationLocal:
		return "refs/heads/" + branch, nil
	case BranchLocationRemoteTracking:
		return "refs/remotes/" + normalizeRemote(remote) + "/" + branch, nil
	default:
		return "", fmt.Errorf("unsupported branch location %q", location)
	}
}

func normalizeBranchLocation(location BranchLocation) BranchLocation {
	if strings.TrimSpace(string(location)) == "" {
		return BranchLocationLocal
	}
	return location
}

func parseWorktrees(raw string) ([]Worktree, error) {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	worktrees := make([]Worktree, 0)
	var current *Worktree

	flush := func() {
		if current == nil {
			return
		}
		worktrees = append(worktrees, *current)
		current = nil
	}

	for _, line := range lines {
		if line == "" {
			flush()
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &Worktree{Path: strings.TrimSpace(strings.TrimPrefix(line, "worktree "))}
		case current == nil:
			return nil, fmt.Errorf("unexpected worktree metadata line %q", line)
		case strings.HasPrefix(line, "HEAD "):
			current.HeadSHA = strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
		case strings.HasPrefix(line, "branch "):
			current.BranchRef = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			current.Branch = strings.TrimPrefix(current.BranchRef, "refs/heads/")
		case line == "detached":
			current.Detached = true
		case line == "bare":
			current.Bare = true
		case strings.HasPrefix(line, "locked"):
			current.Locked = true
			current.LockReason = strings.TrimSpace(strings.TrimPrefix(line, "locked"))
		case strings.HasPrefix(line, "prunable"):
			current.Prunable = true
			current.PrunableReason = strings.TrimSpace(strings.TrimPrefix(line, "prunable"))
		}
	}
	flush()

	return worktrees, nil
}
