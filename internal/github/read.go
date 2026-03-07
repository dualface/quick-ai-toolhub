package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultIssueLimit = 100
	defaultPRLimit    = 100
	defaultRunLimit   = 20
	apiPageSize       = 100
)

var (
	issueJSONFields = []string{
		"number",
		"id",
		"title",
		"body",
		"state",
		"url",
		"labels",
		"createdAt",
		"updatedAt",
		"closedAt",
	}
	pullRequestJSONFields = []string{
		"number",
		"id",
		"title",
		"body",
		"state",
		"url",
		"labels",
		"headRefName",
		"headRefOid",
		"baseRefName",
		"isDraft",
		"mergeStateStatus",
		"autoMergeRequest",
		"createdAt",
		"updatedAt",
		"closedAt",
		"mergedAt",
	}
	workflowRunJSONFields = []string{
		"databaseId",
		"number",
		"name",
		"workflowName",
		"displayTitle",
		"event",
		"headBranch",
		"headSha",
		"status",
		"conclusion",
		"url",
		"createdAt",
		"startedAt",
		"updatedAt",
		"workflowDatabaseId",
	}
	apiHeaders = []string{
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
	}
)

type ghLabel struct {
	Name string `json:"name"`
}

type ghIssue struct {
	Number    int       `json:"number"`
	NodeID    string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	Labels    []ghLabel `json:"labels"`
	CreatedAt string    `json:"createdAt"`
	UpdatedAt string    `json:"updatedAt"`
	ClosedAt  *string   `json:"closedAt"`
}

type ghPullRequest struct {
	Number           int       `json:"number"`
	NodeID           string    `json:"id"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	State            string    `json:"state"`
	URL              string    `json:"url"`
	Labels           []ghLabel `json:"labels"`
	HeadRefName      string    `json:"headRefName"`
	HeadRefOID       string    `json:"headRefOid"`
	BaseRefName      string    `json:"baseRefName"`
	IsDraft          bool      `json:"isDraft"`
	MergeStateStatus string    `json:"mergeStateStatus"`
	AutoMergeRequest *struct{} `json:"autoMergeRequest"`
	CreatedAt        string    `json:"createdAt"`
	UpdatedAt        string    `json:"updatedAt"`
	ClosedAt         *string   `json:"closedAt"`
	MergedAt         *string   `json:"mergedAt"`
}

type ghWorkflowRun struct {
	DatabaseID         int64   `json:"databaseId"`
	Number             int64   `json:"number"`
	Name               string  `json:"name"`
	WorkflowName       *string `json:"workflowName"`
	DisplayTitle       *string `json:"displayTitle"`
	Event              string  `json:"event"`
	HeadBranch         string  `json:"headBranch"`
	HeadSHA            string  `json:"headSha"`
	Status             string  `json:"status"`
	Conclusion         *string `json:"conclusion"`
	URL                string  `json:"url"`
	CreatedAt          string  `json:"createdAt"`
	StartedAt          *string `json:"startedAt"`
	UpdatedAt          string  `json:"updatedAt"`
	WorkflowDatabaseID int64   `json:"workflowDatabaseId"`
}

type ghAPIIssueLink struct {
	Number int     `json:"number"`
	NodeID *string `json:"node_id"`
	Title  string  `json:"title"`
	URL    string  `json:"html_url"`
}

func (c *Client) ListSprintIssues(ctx context.Context, req ListSprintIssuesRequest) ([]Issue, error) {
	return c.ListIssues(ctx, ListIssuesRequest{
		Scope:  req.Scope,
		State:  "open",
		Labels: []string{"kind/sprint"},
		Limit:  req.Limit,
	})
}

func (c *Client) ListIssues(ctx context.Context, req ListIssuesRequest) ([]Issue, error) {
	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "issue", "list",
		"--json", strings.Join(issueJSONFields, ","),
		"--state", defaultString(req.State, "open"),
		"--limit", strconv.Itoa(normalizeLimit(req.Limit, defaultIssueLimit)),
	}
	args = append(args, repeatedFlag("--label", req.Labels)...)
	if search := strings.TrimSpace(req.Search); search != "" {
		args = append(args, "--search", search)
	}
	args = append(args, repoArgs(scope)...)

	var raw []ghIssue
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return normalizeIssues(raw), nil
}

func (c *Client) GetIssue(ctx context.Context, req GetIssueRequest) (Issue, error) {
	if req.GitHubIssueNumber <= 0 {
		return Issue{}, errors.New("github_issue_number must be greater than zero")
	}

	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "issue", "view", strconv.Itoa(req.GitHubIssueNumber),
		"--json", strings.Join(issueJSONFields, ","),
	}
	args = append(args, repoArgs(scope)...)

	var raw ghIssue
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return Issue{}, fmt.Errorf("get issue %d: %w", req.GitHubIssueNumber, err)
	}
	return normalizeIssue(raw), nil
}

func (c *Client) ListSubIssues(ctx context.Context, req ListSubIssuesRequest) ([]IssueLink, error) {
	if req.ParentIssueNumber <= 0 {
		return nil, errors.New("parent_issue_number must be greater than zero")
	}

	endpoint := fmt.Sprintf("repos/{owner}/{repo}/issues/%d/sub_issues", req.ParentIssueNumber)
	items, err := c.listAPIIssueLinks(ctx, req.Scope, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list sub-issues for issue %d: %w", req.ParentIssueNumber, err)
	}
	return items, nil
}

func (c *Client) ListIssueDependencies(ctx context.Context, req ListIssueDependenciesRequest) ([]IssueLink, error) {
	if req.GitHubIssueNumber <= 0 {
		return nil, errors.New("github_issue_number must be greater than zero")
	}

	endpoint := fmt.Sprintf("repos/{owner}/{repo}/issues/%d/dependencies/blocked_by", req.GitHubIssueNumber)
	items, err := c.listAPIIssueLinks(ctx, req.Scope, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list dependencies for issue %d: %w", req.GitHubIssueNumber, err)
	}
	return items, nil
}

func (c *Client) ListPullRequests(ctx context.Context, req ListPullRequestsRequest) ([]PullRequest, error) {
	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "pr", "list",
		"--json", strings.Join(pullRequestJSONFields, ","),
		"--state", defaultString(req.State, "open"),
		"--limit", strconv.Itoa(normalizeLimit(req.Limit, defaultPRLimit)),
	}
	if base := strings.TrimSpace(req.BaseBranch); base != "" {
		args = append(args, "--base", base)
	}
	if head := strings.TrimSpace(req.HeadBranch); head != "" {
		args = append(args, "--head", head)
	}
	args = append(args, repeatedFlag("--label", req.Labels)...)
	if search := strings.TrimSpace(req.Search); search != "" {
		args = append(args, "--search", search)
	}
	args = append(args, repoArgs(scope)...)

	var raw []ghPullRequest
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	return normalizePullRequests(raw), nil
}

func (c *Client) GetPullRequest(ctx context.Context, req GetPullRequestRequest) (PullRequest, error) {
	if req.GitHubPRNumber <= 0 {
		return PullRequest{}, errors.New("github_pr_number must be greater than zero")
	}

	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "pr", "view", strconv.Itoa(req.GitHubPRNumber),
		"--json", strings.Join(pullRequestJSONFields, ","),
	}
	args = append(args, repoArgs(scope)...)

	var raw ghPullRequest
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return PullRequest{}, fmt.Errorf("get pull request %d: %w", req.GitHubPRNumber, err)
	}
	return normalizePullRequest(raw), nil
}

func (c *Client) ListWorkflowRuns(ctx context.Context, req ListWorkflowRunsRequest) ([]WorkflowRun, error) {
	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "run", "list",
		"--json", strings.Join(workflowRunJSONFields, ","),
		"--limit", strconv.Itoa(normalizeLimit(req.Limit, defaultRunLimit)),
	}
	if branch := strings.TrimSpace(req.Branch); branch != "" {
		args = append(args, "--branch", branch)
	}
	if commit := strings.TrimSpace(req.CommitSHA); commit != "" {
		args = append(args, "--commit", commit)
	}
	if event := strings.TrimSpace(req.Event); event != "" {
		args = append(args, "--event", event)
	}
	if status := strings.TrimSpace(req.Status); status != "" {
		args = append(args, "--status", status)
	}
	if workflow := strings.TrimSpace(req.Workflow); workflow != "" {
		args = append(args, "--workflow", workflow)
	}
	args = append(args, repoArgs(scope)...)

	var raw []ghWorkflowRun
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return nil, fmt.Errorf("list workflow runs: %w", err)
	}
	return normalizeWorkflowRuns(raw), nil
}

func (c *Client) GetWorkflowRun(ctx context.Context, req GetWorkflowRunRequest) (WorkflowRun, error) {
	if req.GitHubRunID <= 0 {
		return WorkflowRun{}, errors.New("github_run_id must be greater than zero")
	}

	scope := normalizeScope(req.Scope)
	args := []string{
		"gh", "run", "view", strconv.FormatInt(req.GitHubRunID, 10),
		"--json", strings.Join(workflowRunJSONFields, ","),
	}
	args = append(args, repoArgs(scope)...)

	var raw ghWorkflowRun
	if err := c.runJSON(ctx, scope.WorkDir, args, &raw); err != nil {
		return WorkflowRun{}, fmt.Errorf("get workflow run %d: %w", req.GitHubRunID, err)
	}
	return normalizeWorkflowRun(raw), nil
}

func (c *Client) listAPIIssueLinks(ctx context.Context, scope Scope, endpoint string) ([]IssueLink, error) {
	scope = normalizeScope(scope)

	resolvedEndpoint, err := resolveAPIEndpoint(scope.Repo, endpoint)
	if err != nil {
		return nil, err
	}

	var raw []ghAPIIssueLink
	for page := 1; ; page++ {
		args := []string{
			"gh", "api", resolvedEndpoint,
			"--method", "GET",
		}
		args = append(args, apiHeaders...)
		args = append(args,
			"-f", fmt.Sprintf("per_page=%d", apiPageSize),
			"-f", fmt.Sprintf("page=%d", page),
		)

		var pageItems []ghAPIIssueLink
		if err := c.runJSON(ctx, scope.WorkDir, args, &pageItems); err != nil {
			return nil, err
		}

		raw = append(raw, pageItems...)
		if len(pageItems) < apiPageSize {
			break
		}
	}

	return normalizeIssueLinks(raw), nil
}

func (c *Client) runJSON(ctx context.Context, workdir string, args []string, dest any) error {
	out, err := c.runner.Run(ctx, workdir, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, dest); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func normalizeIssues(raw []ghIssue) []Issue {
	issues := make([]Issue, 0, len(raw))
	for _, item := range raw {
		issues = append(issues, normalizeIssue(item))
	}
	return issues
}

func normalizeIssue(raw ghIssue) Issue {
	return Issue{
		GitHubIssueNumber: raw.Number,
		GitHubIssueNodeID: strings.TrimSpace(raw.NodeID),
		Title:             raw.Title,
		Body:              raw.Body,
		State:             normalizeState(raw.State),
		URL:               raw.URL,
		Labels:            labelNames(raw.Labels),
		CreatedAt:         raw.CreatedAt,
		UpdatedAt:         raw.UpdatedAt,
		ClosedAt:          optionalString(raw.ClosedAt),
	}
}

func normalizeIssueLinks(raw []ghAPIIssueLink) []IssueLink {
	items := make([]IssueLink, 0, len(raw))
	for _, item := range raw {
		items = append(items, IssueLink{
			GitHubIssueNumber: item.Number,
			GitHubIssueNodeID: optionalString(item.NodeID),
			Title:             item.Title,
			URL:               item.URL,
		})
	}
	return items
}

func normalizePullRequests(raw []ghPullRequest) []PullRequest {
	items := make([]PullRequest, 0, len(raw))
	for _, item := range raw {
		items = append(items, normalizePullRequest(item))
	}
	return items
}

func normalizePullRequest(raw ghPullRequest) PullRequest {
	state := normalizeState(raw.State)
	if optionalString(raw.MergedAt) != "" {
		state = "merged"
	}

	return PullRequest{
		GitHubPRNumber:   raw.Number,
		GitHubPRNodeID:   strings.TrimSpace(raw.NodeID),
		Title:            raw.Title,
		Body:             raw.Body,
		State:            state,
		URL:              raw.URL,
		Labels:           labelNames(raw.Labels),
		HeadBranch:       raw.HeadRefName,
		HeadSHA:          raw.HeadRefOID,
		BaseBranch:       raw.BaseRefName,
		IsDraft:          raw.IsDraft,
		MergeStateStatus: normalizeState(raw.MergeStateStatus),
		AutoMergeEnabled: raw.AutoMergeRequest != nil,
		CreatedAt:        raw.CreatedAt,
		UpdatedAt:        raw.UpdatedAt,
		ClosedAt:         optionalString(raw.ClosedAt),
		MergedAt:         optionalString(raw.MergedAt),
	}
}

func normalizeWorkflowRuns(raw []ghWorkflowRun) []WorkflowRun {
	items := make([]WorkflowRun, 0, len(raw))
	for _, item := range raw {
		items = append(items, normalizeWorkflowRun(item))
	}
	return items
}

func normalizeWorkflowRun(raw ghWorkflowRun) WorkflowRun {
	return WorkflowRun{
		GitHubRunID:      raw.DatabaseID,
		RunNumber:        raw.Number,
		Name:             raw.Name,
		WorkflowName:     optionalString(raw.WorkflowName),
		DisplayTitle:     optionalString(raw.DisplayTitle),
		Event:            normalizeState(raw.Event),
		HeadBranch:       raw.HeadBranch,
		HeadSHA:          raw.HeadSHA,
		Status:           normalizeState(raw.Status),
		Conclusion:       normalizeState(optionalString(raw.Conclusion)),
		URL:              raw.URL,
		CreatedAt:        raw.CreatedAt,
		StartedAt:        optionalString(raw.StartedAt),
		UpdatedAt:        raw.UpdatedAt,
		WorkflowDatabase: raw.WorkflowDatabaseID,
	}
}

func normalizeScope(scope Scope) Scope {
	scope.WorkDir = strings.TrimSpace(scope.WorkDir)
	scope.Repo = strings.TrimSpace(scope.Repo)
	if scope.WorkDir == "" {
		scope.WorkDir = "."
	}
	return scope
}

func repoArgs(scope Scope) []string {
	if scope.Repo == "" {
		return nil
	}
	return []string{"-R", scope.Repo}
}

func repeatedFlag(flag string, values []string) []string {
	args := make([]string, 0, len(values)*2)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		args = append(args, flag, value)
	}
	return args
}

func normalizeLimit(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func labelNames(labels []ghLabel) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		name := strings.TrimSpace(label.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeState(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func resolveAPIEndpoint(repo, endpoint string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return endpoint, nil
	}

	parts := strings.Split(repo, "/")
	switch len(parts) {
	case 2:
		endpoint = strings.ReplaceAll(endpoint, "{owner}", parts[0])
		endpoint = strings.ReplaceAll(endpoint, "{repo}", parts[1])
	case 3:
		endpoint = strings.ReplaceAll(endpoint, "{owner}", parts[1])
		endpoint = strings.ReplaceAll(endpoint, "{repo}", parts[2])
	default:
		return "", fmt.Errorf("invalid repo %q", repo)
	}

	return endpoint, nil
}
