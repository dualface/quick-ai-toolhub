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
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if !strings.Contains(string(result.Stdout), `"type":"thread.started"`) {
		t.Fatalf("unexpected stdout capture: %q", string(result.Stdout))
	}

	got := progress.String()
	for _, needle := range []string{
		"[progress] agent started",
		"[progress] todo 1/2, current: second",
		"[progress] agent finished",
	} {
		if !strings.Contains(got, needle) {
			t.Fatalf("missing %q in progress output: %q", needle, got)
		}
	}
}
