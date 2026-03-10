package issuesync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParserLoad(t *testing.T) {
	dir := t.TempDir()

	planPath := filepath.Join(dir, "SPRINTS-V1.md")
	tasksDir := filepath.Join(dir, "tasks", "Sprint-01")

	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	planContent := "# SPRINTS-V1\n\n" +
		"## [Sprint-01] Example Sprint\n\n" +
		"### Goal\n\n" +
		"Example goal.\n\n" +
		"### Done When\n\n" +
		"- done one\n" +
		"- done two\n\n" +
		"### Tasks\n\n" +
		"| task_id | 标题 | 交付 |\n" +
		"| --- | --- | --- |\n" +
		"| `Task-01` | Example Task | Example deliverable |\n"
	if err := os.WriteFile(planPath, []byte(planContent), 0o644); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(tasksDir, "Task-01.md")
	taskContent := `# [Sprint-01][Task-01] Example Task

## Goal

Do the thing.

## Reads

- PROJECT-DEVELOPER-GUIDE.md

## Dependencies

- None

## In Scope

- Implement it

## Out of Scope

- Not this

## Deliverables

- Code

## Acceptance Criteria

- Works

## Notes

- None
`
	if err := os.WriteFile(taskPath, []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}

	parser := Parser{}
	plan, err := parser.Load(planPath, filepath.Join(dir, "tasks"))
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}

	if len(plan.Sprints) != 1 {
		t.Fatalf("expected 1 sprint, got %d", len(plan.Sprints))
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(plan.Tasks))
	}

	task := plan.Tasks["Sprint-01/Task-01"]
	if task == nil {
		t.Fatal("expected task brief")
	}
	if task.Title != "Example Task" {
		t.Fatalf("unexpected title %q", task.Title)
	}
	if len(task.Dependencies) != 0 {
		t.Fatalf("expected no dependencies, got %v", task.Dependencies)
	}
}

func TestParserLoadIgnoresNonTaskTablesAfterSprintSections(t *testing.T) {
	dir := t.TempDir()

	planPath := filepath.Join(dir, "SPRINTS-V1.md")
	tasksDir := filepath.Join(dir, "tasks", "Sprint-06")

	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	planContent := "# SPRINTS-V1\n\n" +
		"## [Sprint-06] Example Sprint\n\n" +
		"### Goal\n\n" +
		"Example goal.\n\n" +
		"### Done When\n\n" +
		"- done one\n\n" +
		"### Tasks\n\n" +
		"| task_id | 标题 | 交付 |\n" +
		"| --- | --- | --- |\n" +
		"| `Task-01` | Example Task | Example deliverable |\n\n" +
		"## Sprint 终态说明\n\n" +
		"| 终态 | 含义 |\n" +
		"| --- | --- |\n" +
		"| `done` | finished |\n"
	if err := os.WriteFile(planPath, []byte(planContent), 0o644); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(tasksDir, "Task-01.md")
	taskContent := `# [Sprint-06][Task-01] Example Task

## Goal

Do the thing.

## Reads

- PROJECT-DEVELOPER-GUIDE.md

## Dependencies

- None

## In Scope

- Implement it

## Out of Scope

- Not this

## Deliverables

- Code

## Acceptance Criteria

- Works

## Notes

- None
`
	if err := os.WriteFile(taskPath, []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}

	parser := Parser{}
	plan, err := parser.Load(planPath, filepath.Join(dir, "tasks"))
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}

	if len(plan.Sprints) != 1 {
		t.Fatalf("expected 1 sprint, got %d", len(plan.Sprints))
	}
	if len(plan.Sprints[0].TaskOrder) != 1 {
		t.Fatalf("expected 1 task summary, got %d", len(plan.Sprints[0].TaskOrder))
	}
	if plan.Sprints[0].TaskOrder[0].TaskLocalID != "Task-01" {
		t.Fatalf("unexpected task summary: %+v", plan.Sprints[0].TaskOrder[0])
	}
}

func TestParserLoadTaskReadsTaskBriefWithoutSprintPlan(t *testing.T) {
	dir := t.TempDir()
	tasksDir := filepath.Join(dir, "tasks", "Sprint-04")

	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	taskPath := filepath.Join(tasksDir, "Task-03.md")
	taskContent := `# [Sprint-04][Task-03] Example Task

## Goal

Do the thing.

## Reads

- TECH-V1.md

## Dependencies

- Sprint-04/Task-02

## In Scope

- Implement it

## Out of Scope

- Not this

## Deliverables

- Code

## Acceptance Criteria

- Works

## Notes

- None
`
	if err := os.WriteFile(taskPath, []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}

	parser := Parser{}
	task, sprint, err := parser.LoadTask(filepath.Join(dir, "tasks"), "Sprint-04/Task-03")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	if task == nil || sprint == nil {
		t.Fatalf("expected task and sprint, got task=%+v sprint=%+v", task, sprint)
	}
	if task.Title != "Example Task" {
		t.Fatalf("unexpected task title %q", task.Title)
	}
	if sprint.ID != "Sprint-04" {
		t.Fatalf("unexpected sprint id %q", sprint.ID)
	}
}

func TestParseDependencyRef(t *testing.T) {
	ref, ok := ParseDependencyRef("Sprint-01/Task-03")
	if !ok {
		t.Fatal("expected dependency ref to parse")
	}
	if ref.TaskID != "Sprint-01/Task-03" {
		t.Fatalf("unexpected task id %q", ref.TaskID)
	}

	ref, ok = ParseDependencyRef("`Sprint-02/Task-04`")
	if !ok {
		t.Fatal("expected backtick dependency ref to parse")
	}
	if ref.TaskID != "Sprint-02/Task-04" {
		t.Fatalf("unexpected backtick task id %q", ref.TaskID)
	}
}
