package issuesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Runner interface {
	Run(ctx context.Context, workdir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workdir

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

type GitHubClient interface {
	EnsureLabel(ctx context.Context, label LabelSpec) error
	FindIssueByTitle(ctx context.Context, label, title string) (IssueRef, bool, error)
	CreateIssue(ctx context.Context, title, body string, labels []string) (IssueRef, error)
	UpdateIssue(ctx context.Context, number int, title, body string, labels []string) (IssueRef, error)
	ListSubIssues(ctx context.Context, parentNumber int) ([]IssueRef, error)
	AddSubIssue(ctx context.Context, parentNumber int, childID int64) error
	ListBlockedBy(ctx context.Context, issueNumber int) ([]IssueRef, error)
	AddBlockedBy(ctx context.Context, issueNumber int, blockerID int64) error
}

type GitHubCLI struct {
	workdir string
	runner  Runner
}

type IssueRef struct {
	Number int    `json:"number"`
	ID     int64  `json:"id"`
	URL    string `json:"html_url"`
	Title  string `json:"title"`
}

type LabelSpec struct {
	Name        string
	Color       string
	Description string
}

func NewGitHubCLI(workdir string, runner Runner) *GitHubCLI {
	return &GitHubCLI{
		workdir: workdir,
		runner:  runner,
	}
}

func (g *GitHubCLI) EnsureLabel(ctx context.Context, label LabelSpec) error {
	_, err := g.runWithRetry(
		ctx,
		[]string{
			"gh", "label", "create", label.Name,
			"--color", label.Color,
			"--description", label.Description,
			"--force",
		},
	)
	return err
}

func (g *GitHubCLI) FindIssueByTitle(ctx context.Context, label, title string) (IssueRef, bool, error) {
	out, err := g.get(ctx, "repos/{owner}/{repo}/issues",
		"-f", "state=all",
		"-f", "labels="+label,
		"-f", "per_page=100",
	)
	if err != nil {
		return IssueRef{}, false, err
	}

	var issues []struct {
		IssueRef
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := json.Unmarshal(out, &issues); err != nil {
		return IssueRef{}, false, fmt.Errorf("parse issue list: %w", err)
	}

	var matches []IssueRef
	for _, issue := range issues {
		if len(issue.PullRequest) > 0 {
			continue
		}
		if issue.Title == title {
			matches = append(matches, issue.IssueRef)
		}
	}

	if len(matches) == 0 {
		return IssueRef{}, false, nil
	}
	if len(matches) > 1 {
		return IssueRef{}, false, fmt.Errorf("multiple issues found with title %q and label %q", title, label)
	}

	return matches[0], true, nil
}

func (g *GitHubCLI) CreateIssue(ctx context.Context, title, body string, labels []string) (IssueRef, error) {
	bodyFile, err := writeTempBody(body)
	if err != nil {
		return IssueRef{}, err
	}
	defer os.Remove(bodyFile)

	args := g.apiArgs("repos/{owner}/{repo}/issues")
	args = append(args,
		"-f", "title="+title,
		"-F", "body=@"+bodyFile,
	)
	for _, label := range labels {
		args = append(args, "-f", "labels[]="+label)
	}

	var out []byte
	for attempt := 0; attempt < 3; attempt++ {
		out, err = g.runner.Run(ctx, g.workdir, args...)
		if err == nil {
			break
		}
		if !isTransientGitHubError(err) {
			return IssueRef{}, err
		}

		if len(labels) > 0 {
			if ref, ok, lookupErr := g.FindIssueByTitle(ctx, labels[0], title); lookupErr == nil && ok {
				return ref, nil
			}
		}

		if attempt == 2 {
			return IssueRef{}, err
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*500*time.Millisecond); err != nil {
			return IssueRef{}, err
		}
	}

	var ref IssueRef
	if err := json.Unmarshal(out, &ref); err != nil {
		return IssueRef{}, fmt.Errorf("parse issue create response: %w", err)
	}
	return ref, nil
}

func (g *GitHubCLI) UpdateIssue(ctx context.Context, number int, title, body string, labels []string) (IssueRef, error) {
	bodyFile, err := writeTempBody(body)
	if err != nil {
		return IssueRef{}, err
	}
	defer os.Remove(bodyFile)

	args := g.apiArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d", number))
	args = append(args,
		"--method", "PATCH",
		"-f", "title="+title,
		"-F", "body=@"+bodyFile,
	)

	out, err := g.runWithRetry(ctx, args)
	if err != nil {
		return IssueRef{}, err
	}

	var ref IssueRef
	if err := json.Unmarshal(out, &ref); err != nil {
		return IssueRef{}, fmt.Errorf("parse issue update response: %w", err)
	}

	if len(labels) > 0 {
		if err := g.ensureIssueLabels(ctx, number, labels); err != nil {
			return IssueRef{}, err
		}
	}

	return ref, nil
}

func (g *GitHubCLI) ListSubIssues(ctx context.Context, parentNumber int) ([]IssueRef, error) {
	out, err := g.get(ctx, fmt.Sprintf("repos/{owner}/{repo}/issues/%d/sub_issues", parentNumber),
		"-f", "per_page=100",
	)
	if err != nil {
		return nil, err
	}

	var issues []IssueRef
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse sub-issues response: %w", err)
	}
	return issues, nil
}

func (g *GitHubCLI) AddSubIssue(ctx context.Context, parentNumber int, childID int64) error {
	args := g.apiArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d/sub_issues", parentNumber))
	args = append(args,
		"--method", "POST",
		"-F", fmt.Sprintf("sub_issue_id=%d", childID),
	)
	_, err := g.runWithRetry(ctx, args)
	if isAlreadyExistsError(err) {
		return nil
	}
	return err
}

func (g *GitHubCLI) ListBlockedBy(ctx context.Context, issueNumber int) ([]IssueRef, error) {
	out, err := g.get(ctx, fmt.Sprintf("repos/{owner}/{repo}/issues/%d/dependencies/blocked_by", issueNumber),
		"-f", "per_page=100",
	)
	if err != nil {
		return nil, err
	}

	var issues []IssueRef
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse blocked_by response: %w", err)
	}
	return issues, nil
}

func (g *GitHubCLI) AddBlockedBy(ctx context.Context, issueNumber int, blockerID int64) error {
	args := g.apiArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d/dependencies/blocked_by", issueNumber))
	args = append(args,
		"--method", "POST",
		"-F", fmt.Sprintf("issue_id=%d", blockerID),
	)
	_, err := g.runWithRetry(ctx, args)
	if isAlreadyExistsError(err) {
		return nil
	}
	return err
}

func writeTempBody(body string) (string, error) {
	file, err := os.CreateTemp("", "toolhub-issue-body-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp body file: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(body); err != nil {
		return "", fmt.Errorf("write temp body file: %w", err)
	}

	return filepath.Clean(file.Name()), nil
}

func (g *GitHubCLI) get(ctx context.Context, endpoint string, args ...string) ([]byte, error) {
	baseArgs := g.apiArgs(endpoint)
	baseArgs = append(baseArgs, "--method", "GET")
	baseArgs = append(baseArgs, args...)

	return g.runWithRetry(ctx, baseArgs)
}

func (g *GitHubCLI) apiArgs(endpoint string) []string {
	return []string{
		"gh", "api", endpoint,
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
	}
}

func (g *GitHubCLI) ensureIssueLabels(ctx context.Context, number int, labels []string) error {
	args := g.apiArgs(fmt.Sprintf("repos/{owner}/{repo}/issues/%d/labels", number))
	args = append(args, "--method", "POST")
	for _, label := range labels {
		args = append(args, "-f", "labels[]="+label)
	}

	_, err := g.runWithRetry(ctx, args)
	return err
}

func (g *GitHubCLI) runWithRetry(ctx context.Context, args []string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		out, err := g.runner.Run(ctx, g.workdir, args...)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isTransientGitHubError(err) || attempt == 2 {
			break
		}
		if err := sleepWithContext(ctx, time.Duration(attempt+1)*500*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(duration):
		return nil
	}
}

func isTransientGitHubError(err error) bool {
	if err == nil {
		return false
	}

	text := strings.ToLower(err.Error())
	transientSubstrings := []string{
		": eof",
		"connection reset by peer",
		"tls handshake timeout",
		"timeout",
		"temporarily unavailable",
		"stream error",
		"http2: client connection lost",
	}

	for _, needle := range transientSubstrings {
		if strings.Contains(text, needle) {
			return true
		}
	}

	return false
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}

	text := strings.ToLower(err.Error())
	return strings.Contains(text, "422") ||
		strings.Contains(text, "already exists") ||
		strings.Contains(text, "already has") ||
		strings.Contains(text, "has already been taken")
}
