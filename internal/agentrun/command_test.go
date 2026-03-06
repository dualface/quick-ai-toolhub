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
