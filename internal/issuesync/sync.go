package issuesync

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Options struct {
	Apply        bool
	PlanFile     string
	TasksDir     string
	ManifestFile string
	WorkDir      string
}

type Syncer struct {
	Client   GitHubClient
	Manifest *Manifest
	Writer   io.Writer
	Options  Options
}

func (s *Syncer) Sync(ctx context.Context, plan *PlanData) error {
	for _, label := range defaultLabels() {
		if err := s.step(fmt.Sprintf("ensure label %s", label.Name), func() error {
			return s.Client.EnsureLabel(ctx, label)
		}); err != nil {
			return err
		}
	}

	for _, sprint := range plan.Sprints {
		if err := s.syncSprint(ctx, sprint); err != nil {
			return err
		}
	}

	for _, task := range OrderedTasks(plan) {
		if err := s.syncTask(ctx, task); err != nil {
			return err
		}
	}

	for _, sprint := range plan.Sprints {
		if err := s.syncSubIssues(ctx, sprint, plan); err != nil {
			return err
		}
	}

	for _, task := range OrderedTasks(plan) {
		if err := s.syncDependencies(ctx, task); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) syncSprint(ctx context.Context, sprint *Sprint) error {
	title, body := RenderSprintIssue(sprint)
	hash := HashDocument(title, body)
	record, ok := s.Manifest.Sprints[sprint.ID]

	if ok && record.BodyHash == hash && record.Title == title {
		s.log("skip sprint %s (no changes)", sprint.ID)
		return nil
	}

	var ref IssueRef
	var err error
	if !ok && s.Options.Apply {
		ref, ok, err = s.Client.FindIssueByTitle(ctx, "kind/sprint", title)
		if err != nil {
			return err
		}
	}

	if ok {
		number := record.IssueNumber
		if number == 0 {
			number = ref.Number
		}
		if number == 0 {
			return fmt.Errorf("missing issue number for sprint %s", sprint.ID)
		}

		if err := s.step(fmt.Sprintf("update sprint issue %s", sprint.ID), func() error {
			var updateErr error
			ref, updateErr = s.Client.UpdateIssue(ctx, number, title, body, []string{"kind/sprint"})
			return updateErr
		}); err != nil {
			return err
		}
	} else {
		if err := s.step(fmt.Sprintf("create sprint issue %s", sprint.ID), func() error {
			var createErr error
			ref, createErr = s.Client.CreateIssue(ctx, title, body, []string{"kind/sprint"})
			return createErr
		}); err != nil {
			return err
		}
	}

	s.Manifest.Sprints[sprint.ID] = IssueRecord{
		IssueNumber: ref.Number,
		IssueID:     ref.ID,
		URL:         ref.URL,
		Title:       title,
		BodyHash:    hash,
		Source:      sprint.Source,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	return s.persistManifest()
}

func (s *Syncer) syncTask(ctx context.Context, task *TaskBrief) error {
	title, body := RenderTaskIssue(task)
	hash := HashDocument(title, body)
	record, ok := s.Manifest.Tasks[task.TaskID]

	if ok && record.BodyHash == hash && record.Title == title {
		s.log("skip task %s (no changes)", task.TaskID)
		return nil
	}

	var ref IssueRef
	var err error
	if !ok && s.Options.Apply {
		ref, ok, err = s.Client.FindIssueByTitle(ctx, "kind/task", title)
		if err != nil {
			return err
		}
	}

	if ok {
		number := record.IssueNumber
		if number == 0 {
			number = ref.Number
		}
		if number == 0 {
			return fmt.Errorf("missing issue number for task %s", task.TaskID)
		}

		if err := s.step(fmt.Sprintf("update task issue %s", task.TaskID), func() error {
			var updateErr error
			ref, updateErr = s.Client.UpdateIssue(ctx, number, title, body, []string{"kind/task"})
			return updateErr
		}); err != nil {
			return err
		}
	} else {
		if err := s.step(fmt.Sprintf("create task issue %s", task.TaskID), func() error {
			var createErr error
			ref, createErr = s.Client.CreateIssue(ctx, title, body, []string{"kind/task"})
			return createErr
		}); err != nil {
			return err
		}
	}

	s.Manifest.Tasks[task.TaskID] = IssueRecord{
		IssueNumber: ref.Number,
		IssueID:     ref.ID,
		URL:         ref.URL,
		Title:       title,
		BodyHash:    hash,
		Source:      task.Source,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	return s.persistManifest()
}

func (s *Syncer) syncSubIssues(ctx context.Context, sprint *Sprint, plan *PlanData) error {
	parent, ok := s.Manifest.Sprints[sprint.ID]
	if !ok {
		return fmt.Errorf("missing sprint issue in manifest for %s", sprint.ID)
	}

	currentChildren := map[int64]struct{}{}
	if s.Options.Apply {
		issues, err := s.Client.ListSubIssues(ctx, parent.IssueNumber)
		if err != nil {
			return err
		}
		for _, issue := range issues {
			currentChildren[issue.ID] = struct{}{}
		}
	}

	for _, summary := range sprint.TaskOrder {
		taskID := fmt.Sprintf("%s/%s", sprint.ID, summary.TaskLocalID)
		taskRecord, ok := s.Manifest.Tasks[taskID]
		if !ok {
			return fmt.Errorf("missing task issue in manifest for %s", taskID)
		}
		if _, ok := currentChildren[taskRecord.IssueID]; ok {
			s.log("skip sub-issue %s -> %s (already linked)", sprint.ID, taskID)
			continue
		}

		task := plan.Tasks[taskID]
		if task == nil {
			return fmt.Errorf("missing task brief for %s", taskID)
		}

		if err := s.step(fmt.Sprintf("link sub-issue %s -> %s", sprint.ID, taskID), func() error {
			return s.Client.AddSubIssue(ctx, parent.IssueNumber, taskRecord.IssueID)
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) syncDependencies(ctx context.Context, task *TaskBrief) error {
	record, ok := s.Manifest.Tasks[task.TaskID]
	if !ok {
		return fmt.Errorf("missing task issue in manifest for %s", task.TaskID)
	}

	currentBlockedBy := map[int64]struct{}{}
	if s.Options.Apply {
		issues, err := s.Client.ListBlockedBy(ctx, record.IssueNumber)
		if err != nil {
			return err
		}
		for _, issue := range issues {
			currentBlockedBy[issue.ID] = struct{}{}
		}
	}

	for _, dependency := range task.Dependencies {
		ref, ok := ParseDependencyRef(dependency)
		if !ok {
			continue
		}

		if ref.SprintID != task.SprintID {
			s.log("skip cross-sprint dependency %s -> %s", task.TaskID, ref.TaskID)
			continue
		}

		blocker, ok := s.Manifest.Tasks[ref.TaskID]
		if !ok {
			return fmt.Errorf("missing dependency issue in manifest for %s", ref.TaskID)
		}
		if _, ok := currentBlockedBy[blocker.IssueID]; ok {
			s.log("skip dependency %s blocked by %s (already linked)", task.TaskID, ref.TaskID)
			continue
		}

		if err := s.step(fmt.Sprintf("link dependency %s blocked by %s", task.TaskID, ref.TaskID), func() error {
			return s.Client.AddBlockedBy(ctx, record.IssueNumber, blocker.IssueID)
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) step(label string, fn func() error) error {
	if !s.Options.Apply {
		s.log("dry-run %s", label)
		return nil
	}
	s.log("apply %s", label)
	return fn()
}

func (s *Syncer) log(format string, args ...any) {
	if s.Writer == nil {
		return
	}
	fmt.Fprintf(s.Writer, format+"\n", args...)
}

func (s *Syncer) persistManifest() error {
	if !s.Options.Apply || s.Options.ManifestFile == "" {
		return nil
	}
	return s.Manifest.Save(s.Options.ManifestFile)
}

func defaultLabels() []LabelSpec {
	return []LabelSpec{
		{Name: "kind/sprint", Color: "1D76DB", Description: "Sprint parent issue"},
		{Name: "kind/task", Color: "0E8A16", Description: "Task issue"},
		{Name: "needs-human", Color: "B60205", Description: "Human intervention required"},
	}
}
