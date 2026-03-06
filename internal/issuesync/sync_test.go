package issuesync

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type fakeGitHubClient struct {
	createIssues      []string
	updateIssues      []string
	subIssueLinks     []string
	dependencyLinks   []string
	labels            []string
	createdIssueCount int
	failAfterCreates  int
}

func (f *fakeGitHubClient) EnsureLabel(_ context.Context, label LabelSpec) error {
	f.labels = append(f.labels, label.Name)
	return nil
}

func (f *fakeGitHubClient) FindIssueByTitle(_ context.Context, _, _ string) (IssueRef, bool, error) {
	return IssueRef{}, false, nil
}

func (f *fakeGitHubClient) CreateIssue(_ context.Context, title, _ string, _ []string) (IssueRef, error) {
	f.createdIssueCount++
	if f.failAfterCreates > 0 && f.createdIssueCount > f.failAfterCreates {
		return IssueRef{}, errors.New("simulated create failure")
	}
	f.createIssues = append(f.createIssues, title)
	return IssueRef{
		Number: f.createdIssueCount,
		ID:     int64(1000 + f.createdIssueCount),
		URL:    "https://example.test/issues",
		Title:  title,
	}, nil
}

func (f *fakeGitHubClient) UpdateIssue(_ context.Context, number int, title, _ string, _ []string) (IssueRef, error) {
	f.updateIssues = append(f.updateIssues, title)
	return IssueRef{Number: number, ID: int64(1000 + number), Title: title, URL: "https://example.test/issues"}, nil
}

func (f *fakeGitHubClient) ListSubIssues(_ context.Context, _ int) ([]IssueRef, error) {
	return nil, nil
}

func (f *fakeGitHubClient) AddSubIssue(_ context.Context, parentNumber int, childID int64) error {
	f.subIssueLinks = append(f.subIssueLinks, "parent")
	_ = parentNumber
	_ = childID
	return nil
}

func (f *fakeGitHubClient) ListBlockedBy(_ context.Context, _ int) ([]IssueRef, error) {
	return nil, nil
}

func (f *fakeGitHubClient) AddBlockedBy(_ context.Context, issueNumber int, blockerID int64) error {
	f.dependencyLinks = append(f.dependencyLinks, "dependency")
	_ = issueNumber
	_ = blockerID
	return nil
}

func TestSyncerApplyCreatesIssuesAndRelationships(t *testing.T) {
	plan := &PlanData{
		Sprints: []*Sprint{
			{
				ID:       "Sprint-01",
				Title:    "Foundation",
				Goal:     "Build foundation",
				DoneWhen: []string{"done"},
				TaskOrder: []TaskSummary{
					{TaskLocalID: "Task-01", Title: "First task"},
					{TaskLocalID: "Task-02", Title: "Second task"},
				},
				Source: "plan/SPRINTS-V1.md",
			},
			{
				ID:       "Sprint-02",
				Title:    "Next",
				Goal:     "Next sprint",
				DoneWhen: []string{"done"},
				TaskOrder: []TaskSummary{
					{TaskLocalID: "Task-01", Title: "Third task"},
				},
				Source: "plan/SPRINTS-V1.md",
			},
		},
		Tasks: map[string]*TaskBrief{
			"Sprint-01/Task-01": {
				SprintID:           "Sprint-01",
				TaskLocalID:        "Task-01",
				TaskID:             "Sprint-01/Task-01",
				Title:              "First task",
				Goal:               "one",
				AcceptanceCriteria: []string{"works"},
				Source:             "plan/tasks/Sprint-01/Task-01.md",
			},
			"Sprint-01/Task-02": {
				SprintID:           "Sprint-01",
				TaskLocalID:        "Task-02",
				TaskID:             "Sprint-01/Task-02",
				Title:              "Second task",
				Goal:               "two",
				Dependencies:       []string{"Sprint-01/Task-01", "Sprint-02/Task-01"},
				AcceptanceCriteria: []string{"works"},
				Source:             "plan/tasks/Sprint-01/Task-02.md",
			},
			"Sprint-02/Task-01": {
				SprintID:           "Sprint-02",
				TaskLocalID:        "Task-01",
				TaskID:             "Sprint-02/Task-01",
				Title:              "Third task",
				Goal:               "three",
				AcceptanceCriteria: []string{"works"},
				Source:             "plan/tasks/Sprint-02/Task-01.md",
			},
		},
	}

	client := &fakeGitHubClient{}
	manifest := &Manifest{
		Version: 1,
		Sprints: map[string]IssueRecord{},
		Tasks:   map[string]IssueRecord{},
	}

	syncer := Syncer{
		Client:   client,
		Manifest: manifest,
		Writer:   io.Discard,
		Options: Options{
			Apply: true,
		},
	}

	if err := syncer.Sync(context.Background(), plan); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(client.createIssues) != 5 {
		t.Fatalf("expected 5 created issues, got %d", len(client.createIssues))
	}
	if len(client.subIssueLinks) != 3 {
		t.Fatalf("expected 3 sub-issue links, got %d", len(client.subIssueLinks))
	}
	if len(client.dependencyLinks) != 1 {
		t.Fatalf("expected 1 same-sprint dependency link, got %d", len(client.dependencyLinks))
	}
	if len(manifest.Sprints) != 2 || len(manifest.Tasks) != 3 {
		t.Fatalf("unexpected manifest sizes: %+v %+v", manifest.Sprints, manifest.Tasks)
	}
}

func TestSyncerPersistsManifestIncrementally(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "issues-manifest.json")

	plan := &PlanData{
		Sprints: []*Sprint{
			{
				ID:       "Sprint-01",
				Title:    "Foundation",
				Goal:     "Build foundation",
				DoneWhen: []string{"done"},
				TaskOrder: []TaskSummary{
					{TaskLocalID: "Task-01", Title: "First task"},
				},
				Source: "plan/SPRINTS-V1.md",
			},
		},
		Tasks: map[string]*TaskBrief{
			"Sprint-01/Task-01": {
				SprintID:           "Sprint-01",
				TaskLocalID:        "Task-01",
				TaskID:             "Sprint-01/Task-01",
				Title:              "First task",
				Goal:               "one",
				AcceptanceCriteria: []string{"works"},
				Source:             "plan/tasks/Sprint-01/Task-01.md",
			},
		},
	}

	client := &fakeGitHubClient{failAfterCreates: 1}
	manifest := &Manifest{
		Version: 1,
		Sprints: map[string]IssueRecord{},
		Tasks:   map[string]IssueRecord{},
	}

	syncer := Syncer{
		Client:   client,
		Manifest: manifest,
		Writer:   io.Discard,
		Options: Options{
			Apply:        true,
			ManifestFile: manifestPath,
		},
	}

	if err := syncer.Sync(context.Background(), plan); err == nil {
		t.Fatal("expected sync to fail after first create")
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("expected manifest to be persisted")
	}

	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load persisted manifest: %v", err)
	}

	if _, ok := loaded.Sprints["Sprint-01"]; !ok {
		t.Fatal("expected persisted sprint record in manifest")
	}
}
