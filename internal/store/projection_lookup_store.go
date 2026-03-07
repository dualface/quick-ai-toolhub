package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s BaseStore) FindTrackedSprintIssueNumbersForIssue(ctx context.Context, githubIssueNumber int) ([]int, error) {
	if githubIssueNumber <= 0 {
		return nil, newValidationError("github_issue_number must be greater than zero")
	}

	db, err := s.requireDB()
	if err != nil {
		return nil, err
	}

	var values []int
	var sprintIssueNumber int
	err = db.NewSelect().
		TableExpr("sprints").
		Column("github_issue_number").
		Where("github_issue_number = ?", githubIssueNumber).
		Limit(1).
		Scan(ctx, &sprintIssueNumber)
	switch {
	case err == nil:
		values = append(values, sprintIssueNumber)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, fmt.Errorf("lookup sprint issue #%d: %w", githubIssueNumber, err)
	}

	var parentIssueNumbers []int
	if err := db.NewSelect().
		TableExpr("tasks").
		Column("parent_github_issue_number").
		Where("github_issue_number = ?", githubIssueNumber).
		Scan(ctx, &parentIssueNumbers); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lookup task issue #%d parent sprint: %w", githubIssueNumber, err)
	}
	values = append(values, parentIssueNumbers...)

	return dedupeInts(values), nil
}

func (s BaseStore) FindTrackedSprintIssueNumberBySprintID(ctx context.Context, sprintID string) (int, bool, error) {
	if strings.TrimSpace(sprintID) == "" {
		return 0, false, newValidationError("sprint_id is required")
	}

	db, err := s.requireDB()
	if err != nil {
		return 0, false, err
	}

	var value int
	err = db.NewSelect().
		TableExpr("sprints").
		Column("github_issue_number").
		Where("sprint_id = ?", sprintID).
		Limit(1).
		Scan(ctx, &value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lookup sprint %s issue number: %w", sprintID, err)
	}
	return value, true, nil
}

func (s BaseStore) FindTrackedSprintIssueNumberByPullRequest(ctx context.Context, githubPRNumber int) (int, bool, error) {
	if githubPRNumber <= 0 {
		return 0, false, newValidationError("github_pr_number must be greater than zero")
	}

	db, err := s.requireDB()
	if err != nil {
		return 0, false, err
	}

	var value int
	err = db.NewSelect().
		TableExpr("pull_requests AS pr").
		ColumnExpr("s.github_issue_number").
		Join("JOIN sprints AS s ON s.sprint_id = pr.sprint_id").
		Where("pr.github_pr_number = ?", githubPRNumber).
		Limit(1).
		Scan(ctx, &value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lookup pull request %d sprint issue number: %w", githubPRNumber, err)
	}
	return value, true, nil
}

func (s BaseStore) FindTrackedSprintIssueNumberByCIRun(ctx context.Context, githubRunID int64) (int, bool, error) {
	if githubRunID <= 0 {
		return 0, false, newValidationError("github_run_id must be greater than zero")
	}

	db, err := s.requireDB()
	if err != nil {
		return 0, false, err
	}

	var value int
	err = db.NewSelect().
		TableExpr("ci_runs AS r").
		ColumnExpr("s.github_issue_number").
		Join("JOIN sprints AS s ON s.sprint_id = r.sprint_id").
		Where("r.github_run_id = ?", githubRunID).
		Limit(1).
		Scan(ctx, &value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lookup ci run %d sprint issue number: %w", githubRunID, err)
	}
	return value, true, nil
}

func (s BaseStore) FindTrackedSprintIssueNumberByHead(ctx context.Context, headSHA, headBranch string) (int, bool, error) {
	if strings.TrimSpace(headSHA) == "" && strings.TrimSpace(headBranch) == "" {
		return 0, false, newValidationError("head_sha or head_branch is required")
	}

	db, err := s.requireDB()
	if err != nil {
		return 0, false, err
	}

	query := db.NewSelect().
		TableExpr("pull_requests AS pr").
		ColumnExpr("s.github_issue_number").
		Join("JOIN sprints AS s ON s.sprint_id = pr.sprint_id").
		OrderExpr("CASE WHEN pr.status = 'open' THEN 0 ELSE 1 END").
		OrderExpr("pr.updated_at DESC").
		Limit(1)

	switch {
	case strings.TrimSpace(headSHA) != "":
		query = query.Where("pr.head_sha = ?", strings.TrimSpace(headSHA))
	case strings.TrimSpace(headBranch) != "":
		query = query.Where("pr.head_branch = ?", strings.TrimSpace(headBranch))
	}

	var value int
	if err := query.Scan(ctx, &value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lookup sprint issue number by head ref: %w", err)
	}
	return value, true, nil
}

func dedupeInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}

	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Service) ApplyGitHubProjection(ctx context.Context, snapshot GitHubProjectionSnapshot) error {
	base, err := s.BaseStore()
	if err != nil {
		return err
	}
	return base.ApplyGitHubProjection(ctx, snapshot)
}

func (s *Service) FindTrackedSprintIssueNumbersForIssue(ctx context.Context, githubIssueNumber int) ([]int, error) {
	base, err := s.BaseStore()
	if err != nil {
		return nil, err
	}
	return base.FindTrackedSprintIssueNumbersForIssue(ctx, githubIssueNumber)
}

func (s *Service) FindTrackedSprintIssueNumberBySprintID(ctx context.Context, sprintID string) (int, bool, error) {
	base, err := s.BaseStore()
	if err != nil {
		return 0, false, err
	}
	return base.FindTrackedSprintIssueNumberBySprintID(ctx, sprintID)
}

func (s *Service) FindTrackedSprintIssueNumberByPullRequest(ctx context.Context, githubPRNumber int) (int, bool, error) {
	base, err := s.BaseStore()
	if err != nil {
		return 0, false, err
	}
	return base.FindTrackedSprintIssueNumberByPullRequest(ctx, githubPRNumber)
}

func (s *Service) FindTrackedSprintIssueNumberByCIRun(ctx context.Context, githubRunID int64) (int, bool, error) {
	base, err := s.BaseStore()
	if err != nil {
		return 0, false, err
	}
	return base.FindTrackedSprintIssueNumberByCIRun(ctx, githubRunID)
}

func (s *Service) FindTrackedSprintIssueNumberByHead(ctx context.Context, headSHA, headBranch string) (int, bool, error) {
	base, err := s.BaseStore()
	if err != nil {
		return 0, false, err
	}
	return base.FindTrackedSprintIssueNumberByHead(ctx, headSHA, headBranch)
}
