package agentrun

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecCommandRunnerStreamsAndCapturesOutput(t *testing.T) {
	var stream bytes.Buffer

	runner := ExecCommandRunner{}
	result, err := runner.Run(context.Background(), CommandRequest{
		Args:         []string{"sh", "-c", "printf 'out'; printf 'err' >&2"},
		StdoutWriter: &stream,
		StderrWriter: &stream,
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if string(result.Stdout) != "out" {
		t.Fatalf("unexpected stdout: %q", string(result.Stdout))
	}
	if string(result.Stderr) != "err" {
		t.Fatalf("unexpected stderr: %q", string(result.Stderr))
	}
	got := stream.String()
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Fatalf("unexpected stream output: %q", got)
	}
}

func TestExecCommandRunnerProgressWriterSummarizesEvents(t *testing.T) {
	var progress bytes.Buffer

	runner := ExecCommandRunner{}
	result, err := runner.Run(context.Background(), CommandRequest{
		Args:           []string{"sh", "-c", "printf '%s\n' '{\"type\":\"thread.started\"}' '{\"type\":\"item.updated\",\"item\":{\"type\":\"todo_list\",\"items\":[{\"text\":\"first\",\"completed\":true},{\"text\":\"second\",\"completed\":false}]}}' '{\"type\":\"turn.completed\"}'"},
		ProgressWriter: &progress,
		Metadata: CommandMetadata{
			Model:   "gpt-5-codex",
			Sandbox: "workspace-write",
			EnvKeys: []string{"TMPDIR", "GOCACHE", "GOMODCACHE", "XDG_CACHE_HOME"},
			EnvSnapshot: map[string]string{
				"TMPDIR":         "/repo/.toolhub/runtime/tmp",
				"GOCACHE":        "/repo/.toolhub/runtime/go-cache",
				"GOMODCACHE":     "/repo/.toolhub/runtime/go-mod-cache",
				"XDG_CACHE_HOME": "/repo/.toolhub/runtime/.cache",
			},
		},
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if !strings.Contains(string(result.Stdout), `"type":"thread.started"`) {
		t.Fatalf("unexpected stdout capture: %q", string(result.Stdout))
	}

	got := progress.String()
	for _, needle := range []string{
		"[progress] model: gpt-5-codex",
		"[progress] sandbox: workspace-write",
		"[progress] env TMPDIR: /repo/.toolhub/runtime/tmp",
		"[progress] env GOCACHE: /repo/.toolhub/runtime/go-cache",
		"[progress] env GOMODCACHE: /repo/.toolhub/runtime/go-mod-cache",
		"[progress] env XDG_CACHE_HOME: /repo/.toolhub/runtime/.cache",
		"[progress] agent started",
		"[progress] todo 1/2, current: second",
		"[progress] agent finished",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in progress output: %q", needle, got)
		}
	}
}

func TestExecCommandRunnerProgressWriterSummarizesClaudeStreamJSON(t *testing.T) {
	var progress bytes.Buffer

	runner := ExecCommandRunner{}
	result, err := runner.Run(context.Background(), CommandRequest{
		Args: []string{"sh", "-c", "printf '%s\n' " +
			"'{\"type\":\"system\",\"subtype\":\"init\"}' " +
			"'{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"StructuredOutput\"}]}}' " +
			"'{\"type\":\"result\",\"subtype\":\"success\",\"structured_output\":{\"status\":\"success\",\"summary\":\"done\",\"next_action\":\"proceed\",\"failure_fingerprint\":null,\"artifact_refs\":{},\"findings\":[]}}'"},
		ProgressWriter: &progress,
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if !strings.Contains(string(result.Stdout), `"type":"system"`) {
		t.Fatalf("unexpected stdout capture: %q", string(result.Stdout))
	}

	got := progress.String()
	for _, needle := range []string{
		"[progress] agent started",
		"[progress] using tool: StructuredOutput",
		"[progress] agent finished",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in progress output: %q", needle, got)
		}
	}
}

func TestExecCommandRunnerStreamOutputsMetadata(t *testing.T) {
	var stream bytes.Buffer

	runner := ExecCommandRunner{}
	_, err := runner.Run(context.Background(), CommandRequest{
		Args:         []string{"sh", "-c", "printf 'out'"},
		StdoutWriter: &stream,
		StderrWriter: &stream,
		Metadata: CommandMetadata{
			Model:   "gpt-5-codex",
			Sandbox: "workspace-write",
			EnvKeys: []string{"TMPDIR"},
			EnvSnapshot: map[string]string{
				"TMPDIR": "/repo/.toolhub/runtime/tmp",
			},
		},
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	got := stream.String()
	for _, needle := range []string{
		"[meta] model: gpt-5-codex",
		"[meta] sandbox: workspace-write",
		"[meta] env TMPDIR: /repo/.toolhub/runtime/tmp",
		"out",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in stream output: %q", needle, got)
		}
	}
}
