package github

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestListSprintIssuesUsesGhIssueListAndNormalizesOutput(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "issue", "list",
					"--json", strings.Join(issueJSONFields, ","),
					"--state", "open",
					"--limit", "50",
					"--label", "kind/sprint",
				},
				stdout: mustJSON(t, []map[string]any{
					{
						"number":    12,
						"id":        "I_kgDOAA",
						"title":     "[Sprint-02] GitHub task projection",
						"body":      "Sprint body",
						"state":     "OPEN",
						"url":       "https://github.com/acme/repo/issues/12",
						"labels":    []map[string]any{{"name": "kind/sprint"}, {"name": "needs-human"}},
						"createdAt": "2026-03-01T00:00:00Z",
						"updatedAt": "2026-03-02T00:00:00Z",
						"closedAt":  nil,
					},
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	issues, err := client.ListSprintIssues(context.Background(), ListSprintIssuesRequest{
		Scope: Scope{WorkDir: "/repo/worktree"},
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("list sprint issues: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	if issues[0].GitHubIssueNumber != 12 {
		t.Fatalf("unexpected issue number: %d", issues[0].GitHubIssueNumber)
	}
	if issues[0].State != "open" {
		t.Fatalf("unexpected issue state: %q", issues[0].State)
	}
	if !issues[0].HasLabel("kind/sprint") || !issues[0].HasLabel("needs-human") {
		t.Fatalf("unexpected labels: %#v", issues[0].Labels)
	}
	if issues[0].ClosedAt != "" {
		t.Fatalf("expected empty closed_at, got %q", issues[0].ClosedAt)
	}

	runner.AssertDone()
}

func TestGetIssueUsesRepoOverride(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/outside",
				wantArgs: []string{
					"gh", "issue", "view", "42",
					"--json", strings.Join(issueJSONFields, ","),
					"-R", "octo/acme",
				},
				stdout: mustJSON(t, map[string]any{
					"number":    42,
					"id":        "I_kgDOBB",
					"title":     "[Sprint-02][Task-01] Build adapter",
					"body":      "Task body",
					"state":     "CLOSED",
					"url":       "https://github.com/octo/acme/issues/42",
					"labels":    []map[string]any{{"name": "kind/task"}},
					"createdAt": "2026-03-01T00:00:00Z",
					"updatedAt": "2026-03-03T00:00:00Z",
					"closedAt":  "2026-03-04T00:00:00Z",
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	issue, err := client.GetIssue(context.Background(), GetIssueRequest{
		Scope:             Scope{WorkDir: "/outside", Repo: "octo/acme"},
		GitHubIssueNumber: 42,
	})
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}

	if issue.GitHubIssueNodeID != "I_kgDOBB" {
		t.Fatalf("unexpected issue node id: %q", issue.GitHubIssueNodeID)
	}
	if issue.State != "closed" {
		t.Fatalf("unexpected issue state: %q", issue.State)
	}
	if issue.ClosedAt != "2026-03-04T00:00:00Z" {
		t.Fatalf("unexpected closed_at: %q", issue.ClosedAt)
	}

	runner.AssertDone()
}

func TestListSubIssuesUsesGhAPIPagination(t *testing.T) {
	pageOne := make([]map[string]any, 0, apiPageSize)
	for i := 0; i < apiPageSize; i++ {
		pageOne = append(pageOne, map[string]any{
			"number":   i + 1,
			"node_id":  "I_sub_" + strconv.Itoa(i+1),
			"title":    "Task " + strconv.Itoa(i+1),
			"html_url": "https://github.com/octo/acme/issues/" + strconv.Itoa(i+1),
		})
	}

	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "api", "repos/octo/acme/issues/77/sub_issues",
					"--method", "GET",
					"-H", "Accept: application/vnd.github+json",
					"-H", "X-GitHub-Api-Version: 2022-11-28",
					"-f", "per_page=100",
					"-f", "page=1",
				},
				stdout: mustJSON(t, pageOne),
			},
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "api", "repos/octo/acme/issues/77/sub_issues",
					"--method", "GET",
					"-H", "Accept: application/vnd.github+json",
					"-H", "X-GitHub-Api-Version: 2022-11-28",
					"-f", "per_page=100",
					"-f", "page=2",
				},
				stdout: mustJSON(t, []map[string]any{
					{
						"number":   101,
						"node_id":  "I_sub_101",
						"title":    "Task 101",
						"html_url": "https://github.com/octo/acme/issues/101",
					},
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	items, err := client.ListSubIssues(context.Background(), ListSubIssuesRequest{
		Scope:             Scope{WorkDir: "/repo/worktree", Repo: "octo/acme"},
		ParentIssueNumber: 77,
	})
	if err != nil {
		t.Fatalf("list sub-issues: %v", err)
	}

	if len(items) != 101 {
		t.Fatalf("got %d sub-issues, want 101", len(items))
	}
	if items[100].GitHubIssueNumber != 101 {
		t.Fatalf("unexpected last sub-issue number: %d", items[100].GitHubIssueNumber)
	}
	if items[100].GitHubIssueNodeID != "I_sub_101" {
		t.Fatalf("unexpected last sub-issue node id: %q", items[100].GitHubIssueNodeID)
	}

	runner.AssertDone()
}

func TestListIssueDependenciesUsesGhAPI(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "api", "repos/{owner}/{repo}/issues/42/dependencies/blocked_by",
					"--method", "GET",
					"-H", "Accept: application/vnd.github+json",
					"-H", "X-GitHub-Api-Version: 2022-11-28",
					"-f", "per_page=100",
					"-f", "page=1",
				},
				stdout: mustJSON(t, []map[string]any{
					{
						"number":   11,
						"node_id":  "I_block_11",
						"title":    "[Sprint-02][Task-00] Prepare inputs",
						"html_url": "https://github.com/acme/repo/issues/11",
					},
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	items, err := client.ListIssueDependencies(context.Background(), ListIssueDependenciesRequest{
		Scope:             Scope{WorkDir: "/repo/worktree"},
		GitHubIssueNumber: 42,
	})
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d dependencies, want 1", len(items))
	}
	if items[0].Title != "[Sprint-02][Task-00] Prepare inputs" {
		t.Fatalf("unexpected dependency title: %q", items[0].Title)
	}

	runner.AssertDone()
}

func TestListPullRequestsNormalizesOutput(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "pr", "list",
					"--json", strings.Join(pullRequestJSONFields, ","),
					"--state", "all",
					"--limit", "25",
					"--base", "sprint/Sprint-02",
					"--head", "task/Sprint-02-Task-01",
					"--label", "kind/task",
					"--search", "is:pr",
					"-R", "octo/acme",
				},
				stdout: mustJSON(t, []map[string]any{
					{
						"number":           88,
						"id":               "PR_kwDOCC",
						"title":            "Sprint-02 Task-01",
						"body":             "PR body",
						"state":            "OPEN",
						"url":              "https://github.com/octo/acme/pull/88",
						"labels":           []map[string]any{{"name": "kind/task"}},
						"headRefName":      "task/Sprint-02-Task-01",
						"headRefOid":       "abc123",
						"baseRefName":      "sprint/Sprint-02",
						"isDraft":          false,
						"mergeStateStatus": "CLEAN",
						"autoMergeRequest": map[string]any{"enabledAt": "2026-03-01T00:00:00Z"},
						"createdAt":        "2026-03-01T00:00:00Z",
						"updatedAt":        "2026-03-02T00:00:00Z",
						"closedAt":         nil,
						"mergedAt":         nil,
					},
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	items, err := client.ListPullRequests(context.Background(), ListPullRequestsRequest{
		Scope:      Scope{WorkDir: "/repo/worktree", Repo: "octo/acme"},
		State:      "all",
		BaseBranch: "sprint/Sprint-02",
		HeadBranch: "task/Sprint-02-Task-01",
		Labels:     []string{"kind/task"},
		Search:     "is:pr",
		Limit:      25,
	})
	if err != nil {
		t.Fatalf("list pull requests: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d pull requests, want 1", len(items))
	}
	if items[0].State != "open" {
		t.Fatalf("unexpected pr state: %q", items[0].State)
	}
	if items[0].MergeStateStatus != "clean" {
		t.Fatalf("unexpected merge state status: %q", items[0].MergeStateStatus)
	}
	if !items[0].AutoMergeEnabled {
		t.Fatal("expected auto merge to be enabled")
	}

	runner.AssertDone()
}

func TestGetPullRequestNormalizesMergedState(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "pr", "view", "88",
					"--json", strings.Join(pullRequestJSONFields, ","),
				},
				stdout: mustJSON(t, map[string]any{
					"number":           88,
					"id":               "PR_kwDOCC",
					"title":            "Sprint-02 Task-01",
					"body":             "PR body",
					"state":            "CLOSED",
					"url":              "https://github.com/octo/acme/pull/88",
					"labels":           []map[string]any{{"name": "kind/task"}},
					"headRefName":      "task/Sprint-02-Task-01",
					"headRefOid":       "abc123",
					"baseRefName":      "sprint/Sprint-02",
					"isDraft":          false,
					"mergeStateStatus": "UNKNOWN",
					"autoMergeRequest": nil,
					"createdAt":        "2026-03-01T00:00:00Z",
					"updatedAt":        "2026-03-03T00:00:00Z",
					"closedAt":         "2026-03-03T00:00:00Z",
					"mergedAt":         "2026-03-03T00:00:00Z",
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	item, err := client.GetPullRequest(context.Background(), GetPullRequestRequest{
		Scope:          Scope{WorkDir: "/repo/worktree"},
		GitHubPRNumber: 88,
	})
	if err != nil {
		t.Fatalf("get pull request: %v", err)
	}

	if item.State != "merged" {
		t.Fatalf("unexpected pr state: %q", item.State)
	}
	if item.MergedAt != "2026-03-03T00:00:00Z" {
		t.Fatalf("unexpected merged_at: %q", item.MergedAt)
	}

	runner.AssertDone()
}

func TestListWorkflowRunsNormalizesOutput(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "run", "list",
					"--json", strings.Join(workflowRunJSONFields, ","),
					"--limit", "10",
					"--branch", "task/Sprint-02-Task-01",
					"--commit", "abc123",
					"--event", "pull_request",
					"--status", "completed",
					"--workflow", "CI",
					"-R", "octo/acme",
				},
				stdout: mustJSON(t, []map[string]any{
					{
						"databaseId":         501,
						"number":             32,
						"name":               "CI / unit",
						"workflowName":       "CI",
						"displayTitle":       "Task checks",
						"event":              "PULL_REQUEST",
						"headBranch":         "task/Sprint-02-Task-01",
						"headSha":            "abc123",
						"status":             "COMPLETED",
						"conclusion":         "SUCCESS",
						"url":                "https://github.com/octo/acme/actions/runs/501",
						"createdAt":          "2026-03-01T00:00:00Z",
						"startedAt":          "2026-03-01T00:01:00Z",
						"updatedAt":          "2026-03-01T00:05:00Z",
						"workflowDatabaseId": 7001,
					},
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	items, err := client.ListWorkflowRuns(context.Background(), ListWorkflowRunsRequest{
		Scope:     Scope{WorkDir: "/repo/worktree", Repo: "octo/acme"},
		Branch:    "task/Sprint-02-Task-01",
		CommitSHA: "abc123",
		Event:     "pull_request",
		Status:    "completed",
		Workflow:  "CI",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("list workflow runs: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d workflow runs, want 1", len(items))
	}
	if items[0].GitHubRunID != 501 {
		t.Fatalf("unexpected run id: %d", items[0].GitHubRunID)
	}
	if items[0].Status != "completed" || items[0].Conclusion != "success" {
		t.Fatalf("unexpected run status/conclusion: %q %q", items[0].Status, items[0].Conclusion)
	}
	if items[0].Event != "pull_request" {
		t.Fatalf("unexpected run event: %q", items[0].Event)
	}

	runner.AssertDone()
}

func TestGetWorkflowRunUsesGhRunView(t *testing.T) {
	runner := &scriptedRunner{
		t: t,
		responses: []scriptedResponse{
			{
				wantWorkDir: "/repo/worktree",
				wantArgs: []string{
					"gh", "run", "view", "501",
					"--json", strings.Join(workflowRunJSONFields, ","),
				},
				stdout: mustJSON(t, map[string]any{
					"databaseId":         501,
					"number":             32,
					"name":               "CI / unit",
					"workflowName":       "CI",
					"displayTitle":       "Task checks",
					"event":              "push",
					"headBranch":         "task/Sprint-02-Task-01",
					"headSha":            "abc123",
					"status":             "IN_PROGRESS",
					"conclusion":         nil,
					"url":                "https://github.com/octo/acme/actions/runs/501",
					"createdAt":          "2026-03-01T00:00:00Z",
					"startedAt":          nil,
					"updatedAt":          "2026-03-01T00:01:00Z",
					"workflowDatabaseId": 7001,
				}),
			},
		},
	}

	client := New(Dependencies{Runner: runner})
	item, err := client.GetWorkflowRun(context.Background(), GetWorkflowRunRequest{
		Scope:       Scope{WorkDir: "/repo/worktree"},
		GitHubRunID: 501,
	})
	if err != nil {
		t.Fatalf("get workflow run: %v", err)
	}

	if item.Status != "in_progress" {
		t.Fatalf("unexpected run status: %q", item.Status)
	}
	if item.Conclusion != "" {
		t.Fatalf("expected empty conclusion, got %q", item.Conclusion)
	}
	if item.StartedAt != "" {
		t.Fatalf("expected empty started_at, got %q", item.StartedAt)
	}

	runner.AssertDone()
}

type scriptedRunner struct {
	t         *testing.T
	responses []scriptedResponse
	callCount int
}

type scriptedResponse struct {
	wantWorkDir string
	wantArgs    []string
	stdout      string
	err         error
}

func (r *scriptedRunner) Run(_ context.Context, workdir string, args ...string) ([]byte, error) {
	r.t.Helper()

	if r.callCount >= len(r.responses) {
		r.t.Fatalf("unexpected extra call %d with args %#v", r.callCount+1, args)
	}

	response := r.responses[r.callCount]
	r.callCount++

	if workdir != response.wantWorkDir {
		r.t.Fatalf("unexpected workdir: got %q want %q", workdir, response.wantWorkDir)
	}
	if !reflect.DeepEqual(args, response.wantArgs) {
		r.t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, response.wantArgs)
	}

	return []byte(response.stdout), response.err
}

func (r *scriptedRunner) AssertDone() {
	r.t.Helper()

	if r.callCount != len(r.responses) {
		r.t.Fatalf("runner consumed %d responses, want %d", r.callCount, len(r.responses))
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}
