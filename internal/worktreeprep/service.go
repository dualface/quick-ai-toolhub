package worktreeprep

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	toolgit "quick-ai-toolhub/internal/git"
)

const defaultRemoteName = "origin"

type GitClient interface {
	Fetch(context.Context, toolgit.FetchRequest) error
	BranchExists(context.Context, toolgit.BranchExistsRequest) (bool, error)
	CreateBranch(context.Context, toolgit.CreateBranchRequest) error
	AddWorktree(context.Context, toolgit.AddWorktreeRequest) error
	ListWorktrees(context.Context, toolgit.ListWorktreesRequest) ([]toolgit.Worktree, error)
	RemoveWorktree(context.Context, toolgit.RemoveWorktreeRequest) error
	RevParse(context.Context, toolgit.RevParseRequest) (string, error)
	MergeBase(context.Context, toolgit.MergeBaseRequest) (string, error)
}

type Service struct {
	logger *slog.Logger
	git    GitClient
}

type Dependencies struct {
	Logger *slog.Logger
	Git    GitClient
}

func New(deps Dependencies) *Service {
	client := deps.Git
	if client == nil {
		client = toolgit.New(toolgit.Dependencies{Logger: deps.Logger})
	}

	return &Service{
		logger: componentLogger(deps.Logger),
		git:    client,
	}
}

func (s *Service) Name() string {
	return "worktreeprep"
}

func (s *Service) Execute(ctx context.Context, req Request, opts ExecuteOptions) Response {
	data, err := s.execute(ctx, req, opts)
	if err != nil {
		return Response{
			OK:    false,
			Error: asToolError(err),
		}
	}

	return Response{
		OK:   true,
		Data: &data,
	}
}

func (s *Service) execute(ctx context.Context, req Request, opts ExecuteOptions) (ResponseData, error) {
	if ctx == nil {
		return ResponseData{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return ResponseData{}, err
	}
	if s.git == nil {
		return ResponseData{}, errors.New("git client is required")
	}

	req = normalizeRequest(req)
	opts = normalizeOptions(opts)
	if err := validateRequest(req); err != nil {
		return ResponseData{}, err
	}
	if err := validateOptions(opts); err != nil {
		return ResponseData{}, err
	}

	repoRoot, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return ResponseData{}, fmt.Errorf("resolve workdir %s: %w", opts.WorkDir, err)
	}

	worktreePath, err := resolveDesiredWorktreePath(repoRoot, req.WorktreeRoot, req.TaskID)
	if err != nil {
		return ResponseData{}, err
	}

	remote := effectiveRemote(opts.Remote)
	if err := s.git.Fetch(ctx, toolgit.FetchRequest{
		WorkDir: repoRoot,
		Remote:  remote,
		Prune:   true,
	}); err != nil {
		return ResponseData{}, fmt.Errorf("fetch remote %s: %w", remote, err)
	}

	if _, err := s.ensureSprintBranch(ctx, repoRoot, req.SprintBranch, opts.DefaultBranch, remote); err != nil {
		return ResponseData{}, err
	}

	branchReused, err := s.ensureTaskBranch(ctx, repoRoot, req.TaskBranch, req.SprintBranch, remote)
	if err != nil {
		return ResponseData{}, err
	}

	actualWorktreePath, worktreeReused, err := s.ensureTaskWorktree(ctx, repoRoot, req.TaskBranch, worktreePath)
	if err != nil {
		return ResponseData{}, err
	}

	baseCommitSHA, err := s.git.MergeBase(ctx, toolgit.MergeBaseRequest{
		WorkDir:       repoRoot,
		LeftRevision:  req.TaskBranch,
		RightRevision: req.SprintBranch,
	})
	if err != nil {
		return ResponseData{}, fmt.Errorf("resolve merge-base for %s and %s: %w", req.TaskBranch, req.SprintBranch, err)
	}

	return ResponseData{
		WorktreePath:  actualWorktreePath,
		TaskBranch:    req.TaskBranch,
		BaseBranch:    req.SprintBranch,
		BaseCommitSHA: baseCommitSHA,
		Reused:        branchReused && worktreeReused,
	}, nil
}

func (s *Service) ensureSprintBranch(ctx context.Context, repoRoot, sprintBranch, defaultBranch, remote string) (string, error) {
	remoteSprintRef := remoteTrackingRef(remote, sprintBranch)
	remoteSprintExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir:  repoRoot,
		Branch:   sprintBranch,
		Location: toolgit.BranchLocationRemoteTracking,
		Remote:   remote,
	})
	if err != nil {
		return "", fmt.Errorf("check remote sprint branch %s: %w", sprintBranch, err)
	}

	localSprintExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir: repoRoot,
		Branch:  sprintBranch,
	})
	if err != nil {
		return "", fmt.Errorf("check local sprint branch %s: %w", sprintBranch, err)
	}

	if remoteSprintExists {
		targetSHA, err := s.git.RevParse(ctx, toolgit.RevParseRequest{
			WorkDir:  repoRoot,
			Revision: remoteSprintRef,
		})
		if err != nil {
			return "", fmt.Errorf("resolve remote sprint branch %s: %w", sprintBranch, err)
		}

		if !localSprintExists {
			if err := s.git.CreateBranch(ctx, toolgit.CreateBranchRequest{
				WorkDir:    repoRoot,
				Branch:     sprintBranch,
				StartPoint: remoteSprintRef,
			}); err != nil {
				return "", fmt.Errorf("create sprint branch %s from %s: %w", sprintBranch, remoteSprintRef, err)
			}
			return targetSHA, nil
		}

		currentSHA, err := s.git.RevParse(ctx, toolgit.RevParseRequest{
			WorkDir:  repoRoot,
			Revision: sprintBranch,
		})
		if err != nil {
			return "", fmt.Errorf("resolve sprint branch %s: %w", sprintBranch, err)
		}
		if currentSHA != targetSHA {
			canFastForward, err := s.canFastForwardBranch(ctx, repoRoot, sprintBranch, remoteSprintRef, currentSHA)
			if err != nil {
				return "", err
			}
			if !canFastForward {
				return "", newConflictError("sprint branch %s has local commits that cannot be fast-forwarded to %s", sprintBranch, remoteSprintRef)
			}

			checkedOutPath, err := s.checkedOutWorktreePath(ctx, repoRoot, sprintBranch)
			if err != nil {
				return "", err
			}
			if checkedOutPath != "" {
				return "", newConflictError("sprint branch %s is checked out in worktree %s and cannot be updated automatically", sprintBranch, checkedOutPath)
			}

			if err := s.git.CreateBranch(ctx, toolgit.CreateBranchRequest{
				WorkDir:    repoRoot,
				Branch:     sprintBranch,
				StartPoint: remoteSprintRef,
				Force:      true,
			}); err != nil {
				return "", fmt.Errorf("fast-forward sprint branch %s to %s: %w", sprintBranch, remoteSprintRef, err)
			}
		}
		return targetSHA, nil
	}

	if localSprintExists {
		headSHA, err := s.git.RevParse(ctx, toolgit.RevParseRequest{
			WorkDir:  repoRoot,
			Revision: sprintBranch,
		})
		if err != nil {
			return "", fmt.Errorf("resolve sprint branch %s: %w", sprintBranch, err)
		}
		return headSHA, nil
	}

	startPoint, err := s.defaultStartPoint(ctx, repoRoot, defaultBranch, remote)
	if err != nil {
		return "", err
	}

	if err := s.git.CreateBranch(ctx, toolgit.CreateBranchRequest{
		WorkDir:    repoRoot,
		Branch:     sprintBranch,
		StartPoint: startPoint,
	}); err != nil {
		return "", fmt.Errorf("create sprint branch %s from %s: %w", sprintBranch, startPoint, err)
	}

	headSHA, err := s.git.RevParse(ctx, toolgit.RevParseRequest{
		WorkDir:  repoRoot,
		Revision: sprintBranch,
	})
	if err != nil {
		return "", fmt.Errorf("resolve sprint branch %s: %w", sprintBranch, err)
	}
	return headSHA, nil
}

func (s *Service) canFastForwardBranch(ctx context.Context, repoRoot, branch, targetRef, currentSHA string) (bool, error) {
	mergeBase, err := s.git.MergeBase(ctx, toolgit.MergeBaseRequest{
		WorkDir:       repoRoot,
		LeftRevision:  branch,
		RightRevision: targetRef,
	})
	if err != nil {
		return false, fmt.Errorf("compare sprint branch %s with %s: %w", branch, targetRef, err)
	}
	return mergeBase == currentSHA, nil
}

func (s *Service) checkedOutWorktreePath(ctx context.Context, repoRoot, branch string) (string, error) {
	worktrees, err := s.git.ListWorktrees(ctx, toolgit.ListWorktreesRequest{WorkDir: repoRoot})
	if err != nil {
		return "", fmt.Errorf("list worktrees for branch %s: %w", branch, err)
	}

	for _, item := range worktrees {
		if item.Branch != branch && item.BranchRef != localBranchRef(branch) {
			continue
		}
		return normalizeListedPath(repoRoot, item.Path), nil
	}
	return "", nil
}

func (s *Service) defaultStartPoint(ctx context.Context, repoRoot, defaultBranch, remote string) (string, error) {
	remoteDefaultExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir:  repoRoot,
		Branch:   defaultBranch,
		Location: toolgit.BranchLocationRemoteTracking,
		Remote:   remote,
	})
	if err != nil {
		return "", fmt.Errorf("check remote default branch %s: %w", defaultBranch, err)
	}
	if remoteDefaultExists {
		return remoteTrackingRef(remote, defaultBranch), nil
	}

	localDefaultExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir: repoRoot,
		Branch:  defaultBranch,
	})
	if err != nil {
		return "", fmt.Errorf("check local default branch %s: %w", defaultBranch, err)
	}
	if !localDefaultExists {
		return "", newValidationError("default branch %s is not available locally or on remote %s", defaultBranch, remote)
	}

	return defaultBranch, nil
}

func (s *Service) ensureTaskBranch(ctx context.Context, repoRoot, taskBranch, sprintBranch, remote string) (bool, error) {
	localTaskExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir: repoRoot,
		Branch:  taskBranch,
	})
	if err != nil {
		return false, fmt.Errorf("check local task branch %s: %w", taskBranch, err)
	}
	if localTaskExists {
		return true, nil
	}

	startPoint := sprintBranch
	branchReused := false

	remoteTaskExists, err := s.git.BranchExists(ctx, toolgit.BranchExistsRequest{
		WorkDir:  repoRoot,
		Branch:   taskBranch,
		Location: toolgit.BranchLocationRemoteTracking,
		Remote:   remote,
	})
	if err != nil {
		return false, fmt.Errorf("check remote task branch %s: %w", taskBranch, err)
	}
	if remoteTaskExists {
		startPoint = remoteTrackingRef(remote, taskBranch)
		branchReused = true
	}

	if err := s.git.CreateBranch(ctx, toolgit.CreateBranchRequest{
		WorkDir:    repoRoot,
		Branch:     taskBranch,
		StartPoint: startPoint,
	}); err != nil {
		return false, fmt.Errorf("create task branch %s from %s: %w", taskBranch, startPoint, err)
	}

	return branchReused, nil
}

func (s *Service) ensureTaskWorktree(ctx context.Context, repoRoot, taskBranch, desiredPath string) (string, bool, error) {
	worktrees, err := s.git.ListWorktrees(ctx, toolgit.ListWorktreesRequest{WorkDir: repoRoot})
	if err != nil {
		return "", false, fmt.Errorf("list worktrees: %w", err)
	}

	refreshed, err := s.removeStaleRelevantWorktrees(ctx, repoRoot, worktrees, taskBranch, desiredPath)
	if err != nil {
		return "", false, err
	}
	worktrees = refreshed

	var pathMatch *toolgit.Worktree
	var branchMatch *toolgit.Worktree
	for i := range worktrees {
		item := worktrees[i]
		normalizedPath := normalizeListedPath(repoRoot, item.Path)
		if normalizedPath == desiredPath {
			pathMatch = &worktrees[i]
		}
		if item.Branch == taskBranch || item.BranchRef == localBranchRef(taskBranch) {
			branchMatch = &worktrees[i]
		}
	}

	if branchMatch != nil {
		actualPath := normalizeListedPath(repoRoot, branchMatch.Path)
		if pathMatch != nil && actualPath != desiredPath && pathMatch.Branch != taskBranch {
			return "", false, newConflictError("worktree path %s is already attached to branch %s", desiredPath, pathMatch.Branch)
		}
		return actualPath, true, nil
	}

	if pathMatch != nil {
		return "", false, newConflictError("worktree path %s is already attached to branch %s", desiredPath, pathMatch.Branch)
	}

	if err := os.MkdirAll(filepath.Dir(desiredPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create worktree parent %s: %w", filepath.Dir(desiredPath), err)
	}
	if err := s.git.AddWorktree(ctx, toolgit.AddWorktreeRequest{
		WorkDir: repoRoot,
		Path:    desiredPath,
		Branch:  taskBranch,
	}); err != nil {
		return "", false, fmt.Errorf("add worktree %s for %s: %w", desiredPath, taskBranch, err)
	}

	return desiredPath, false, nil
}

func (s *Service) removeStaleRelevantWorktrees(ctx context.Context, repoRoot string, worktrees []toolgit.Worktree, taskBranch, desiredPath string) ([]toolgit.Worktree, error) {
	removed := false

	for _, item := range worktrees {
		normalizedPath := normalizeListedPath(repoRoot, item.Path)
		matchesTaskBranch := item.Branch == taskBranch || item.BranchRef == localBranchRef(taskBranch)
		matchesDesiredPath := normalizedPath == desiredPath
		if !matchesTaskBranch && !matchesDesiredPath {
			continue
		}

		stale, err := isStaleWorktree(item, normalizedPath)
		if err != nil {
			return nil, fmt.Errorf("inspect worktree %s: %w", normalizedPath, err)
		}
		if !stale {
			continue
		}

		if err := s.git.RemoveWorktree(ctx, toolgit.RemoveWorktreeRequest{
			WorkDir: repoRoot,
			Path:    item.Path,
			Force:   true,
		}); err != nil {
			return nil, fmt.Errorf("remove stale worktree %s: %w", item.Path, err)
		}
		removed = true
	}

	if !removed {
		return worktrees, nil
	}

	refreshed, err := s.git.ListWorktrees(ctx, toolgit.ListWorktreesRequest{WorkDir: repoRoot})
	if err != nil {
		return nil, fmt.Errorf("list worktrees after cleanup: %w", err)
	}
	return refreshed, nil
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "worktreeprep")
	}
	return logger.With("component", "worktreeprep")
}

func asToolError(err error) *ToolError {
	if err == nil {
		return nil
	}

	var validationErr *validationError
	if errors.As(err, &validationErr) {
		return &ToolError{
			Code:      ErrorCodeInvalid,
			Message:   err.Error(),
			Retryable: false,
		}
	}

	var conflictErr *conflictError
	if errors.As(err, &conflictErr) {
		return &ToolError{
			Code:      ErrorCodeConflict,
			Message:   err.Error(),
			Retryable: false,
		}
	}

	message := err.Error()
	code := ErrorCodeInternal
	retryable := false
	if strings.Contains(message, "git ") ||
		strings.Contains(message, "fetch remote") ||
		strings.Contains(message, "branch ") ||
		strings.Contains(message, "worktree") ||
		strings.Contains(message, "merge-base") {
		code = ErrorCodeGit
		retryable = true
	}

	return &ToolError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
}

func normalizeRequest(req Request) Request {
	req.SprintID = strings.TrimSpace(req.SprintID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.SprintBranch = strings.TrimSpace(req.SprintBranch)
	req.TaskBranch = strings.TrimSpace(req.TaskBranch)
	req.WorktreeRoot = strings.TrimSpace(req.WorktreeRoot)
	return req
}

func normalizeOptions(opts ExecuteOptions) ExecuteOptions {
	opts.WorkDir = strings.TrimSpace(opts.WorkDir)
	opts.DefaultBranch = strings.TrimSpace(opts.DefaultBranch)
	opts.Remote = strings.TrimSpace(opts.Remote)
	return opts
}

func validateRequest(req Request) error {
	if req.SprintID == "" {
		return newValidationError("sprint_id is required")
	}
	if req.TaskID == "" {
		return newValidationError("task_id is required")
	}
	if !strings.HasPrefix(req.TaskID, req.SprintID+"/") {
		return newValidationError("task_id %s must belong to sprint %s", req.TaskID, req.SprintID)
	}
	if req.SprintBranch == "" {
		return newValidationError("sprint_branch is required")
	}
	expectedSprintBranch := "sprint/" + req.SprintID
	if req.SprintBranch != expectedSprintBranch {
		return newValidationError("sprint_branch must be %s", expectedSprintBranch)
	}
	if req.TaskBranch == "" {
		return newValidationError("task_branch is required")
	}
	expectedTaskBranch := "task/" + req.TaskID
	if req.TaskBranch != expectedTaskBranch {
		return newValidationError("task_branch must be %s", expectedTaskBranch)
	}

	if _, err := relativeTaskPath(req.TaskID); err != nil {
		return err
	}
	return nil
}

func validateOptions(opts ExecuteOptions) error {
	if opts.WorkDir == "" {
		return newValidationError("workdir is required")
	}
	if opts.DefaultBranch == "" {
		return newValidationError("default_branch is required")
	}
	return nil
}

func resolveDesiredWorktreePath(repoRoot, worktreeRoot, taskID string) (string, error) {
	root := worktreeRoot
	if root == "" {
		root = filepath.Join(filepath.Dir(repoRoot), "worktrees", filepath.Base(repoRoot))
	} else if !filepath.IsAbs(root) {
		root = filepath.Join(repoRoot, root)
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", newValidationError("resolve worktree_root %s: %v", worktreeRoot, err)
	}

	relativeTaskPath, err := relativeTaskPath(taskID)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(absoluteRoot, relativeTaskPath)), nil
}

func relativeTaskPath(taskID string) (string, error) {
	segments := strings.Split(taskID, "/")
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		switch segment {
		case "", ".", "..":
			return "", newValidationError("task_id %s contains an invalid path segment", taskID)
		}
		if strings.ContainsAny(segment, `/\`) {
			return "", newValidationError("task_id %s contains an invalid path separator", taskID)
		}
		cleaned = append(cleaned, segment)
	}
	return filepath.Join(cleaned...), nil
}

func effectiveRemote(remote string) string {
	if remote == "" {
		return defaultRemoteName
	}
	return remote
}

func remoteTrackingRef(remote, branch string) string {
	return "refs/remotes/" + effectiveRemote(remote) + "/" + branch
}

func localBranchRef(branch string) string {
	return "refs/heads/" + branch
}

func normalizeListedPath(repoRoot, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(repoRoot, path))
}

func isStaleWorktree(item toolgit.Worktree, normalizedPath string) (bool, error) {
	if item.Prunable {
		return true, nil
	}
	info, err := os.Stat(normalizedPath)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}
