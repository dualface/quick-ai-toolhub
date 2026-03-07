package github

import "strings"

type Scope struct {
	WorkDir string
	Repo    string
}

type Issue struct {
	GitHubIssueNumber int
	GitHubIssueNodeID string
	Title             string
	Body              string
	State             string
	URL               string
	Labels            []string
	CreatedAt         string
	UpdatedAt         string
	ClosedAt          string
}

func (i Issue) HasLabel(name string) bool {
	for _, label := range i.Labels {
		if strings.EqualFold(label, name) {
			return true
		}
	}
	return false
}

type IssueLink struct {
	GitHubIssueNumber int
	GitHubIssueNodeID string
	Title             string
	URL               string
}

type PullRequest struct {
	GitHubPRNumber   int
	GitHubPRNodeID   string
	Title            string
	Body             string
	State            string
	URL              string
	Labels           []string
	HeadBranch       string
	HeadSHA          string
	BaseBranch       string
	IsDraft          bool
	MergeStateStatus string
	AutoMergeEnabled bool
	CreatedAt        string
	UpdatedAt        string
	ClosedAt         string
	MergedAt         string
}

type WorkflowRun struct {
	GitHubRunID      int64
	RunNumber        int64
	Name             string
	WorkflowName     string
	DisplayTitle     string
	Event            string
	HeadBranch       string
	HeadSHA          string
	Status           string
	Conclusion       string
	URL              string
	CreatedAt        string
	StartedAt        string
	UpdatedAt        string
	WorkflowDatabase int64
}

type ListSprintIssuesRequest struct {
	Scope Scope
	Limit int
}

type ListIssuesRequest struct {
	Scope  Scope
	State  string
	Labels []string
	Search string
	Limit  int
}

type GetIssueRequest struct {
	Scope             Scope
	GitHubIssueNumber int
}

type ListSubIssuesRequest struct {
	Scope             Scope
	ParentIssueNumber int
}

type ListIssueDependenciesRequest struct {
	Scope             Scope
	GitHubIssueNumber int
}

type ListPullRequestsRequest struct {
	Scope      Scope
	State      string
	BaseBranch string
	HeadBranch string
	Labels     []string
	Search     string
	Limit      int
}

type GetPullRequestRequest struct {
	Scope          Scope
	GitHubPRNumber int
}

type ListWorkflowRunsRequest struct {
	Scope     Scope
	Branch    string
	CommitSHA string
	Event     string
	Status    string
	Workflow  string
	Limit     int
}

type GetWorkflowRunRequest struct {
	Scope       Scope
	GitHubRunID int64
}
