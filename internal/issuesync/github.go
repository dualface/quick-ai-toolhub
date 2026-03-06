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
	"strconv"
	"strings"
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
	_, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "label", "create", label.Name,
		"--color", label.Color,
		"--description", label.Description,
		"--force",
	)
	return err
}

func (g *GitHubCLI) FindIssueByTitle(ctx context.Context, label, title string) (IssueRef, bool, error) {
	out, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", "repos/{owner}/{repo}/issues",
		"-X", "GET",
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

	args := []string{
		"gh", "api", "repos/{owner}/{repo}/issues",
		"-f", "title=" + title,
		"-F", "body=@" + bodyFile,
	}
	for _, label := range labels {
		args = append(args, "-f", "labels[]="+label)
	}

	out, err := g.runner.Run(ctx, g.workdir, args...)
	if err != nil {
		return IssueRef{}, err
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

	args := []string{
		"gh", "issue", "edit", strconv.Itoa(number),
		"--title", title,
		"--body-file", bodyFile,
	}
	for _, label := range labels {
		args = append(args, "--add-label", label)
	}

	if _, err := g.runner.Run(ctx, g.workdir, args...); err != nil {
		return IssueRef{}, err
	}

	return g.getIssue(ctx, number)
}

func (g *GitHubCLI) ListSubIssues(ctx context.Context, parentNumber int) ([]IssueRef, error) {
	out, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/sub_issues", parentNumber),
		"-X", "GET",
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
	_, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/sub_issues", parentNumber),
		"--method", "POST",
		"-F", fmt.Sprintf("sub_issue_id=%d", childID),
	)
	return err
}

func (g *GitHubCLI) ListBlockedBy(ctx context.Context, issueNumber int) ([]IssueRef, error) {
	out, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/dependencies/blocked_by", issueNumber),
		"-X", "GET",
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
	_, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/dependencies/blocked_by", issueNumber),
		"--method", "POST",
		"-F", fmt.Sprintf("issue_id=%d", blockerID),
	)
	return err
}

func (g *GitHubCLI) getIssue(ctx context.Context, number int) (IssueRef, error) {
	out, err := g.runner.Run(
		ctx,
		g.workdir,
		"gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d", number),
	)
	if err != nil {
		return IssueRef{}, err
	}

	var ref IssueRef
	if err := json.Unmarshal(out, &ref); err != nil {
		return IssueRef{}, fmt.Errorf("parse issue response: %w", err)
	}
	return ref, nil
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
