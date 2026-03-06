package agentrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type CommandRequest struct {
	WorkDir      string
	Args         []string
	Stdin        []byte
	StdoutWriter io.Writer
	StderrWriter io.Writer
}

type CommandResult struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(ctx context.Context, req CommandRequest) (CommandResult, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if len(req.Args) == 0 {
		return CommandResult{}, errors.New("missing command")
	}

	cmd := exec.CommandContext(ctx, req.Args[0], req.Args[1:]...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = bytes.NewReader(req.Stdin)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = selectOutputWriter(&stdout, req.StdoutWriter)
	cmd.Stderr = selectOutputWriter(&stderr, req.StderrWriter)

	err := cmd.Run()
	result := CommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if err != nil {
		return result, fmt.Errorf("%s: %w: %s", strings.Join(req.Args, " "), err, formatCommandFailure(stdout.String(), stderr.String()))
	}

	return result, nil
}

func selectOutputWriter(buffer *bytes.Buffer, stream io.Writer) io.Writer {
	if stream == nil {
		return buffer
	}
	return io.MultiWriter(buffer, stream)
}

func formatCommandFailure(stdout, stderr string) string {
	var parts []string

	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	if stderr != "" {
		parts = append(parts, "stderr: "+stderr)
	}
	if stdout != "" {
		parts = append(parts, "stdout: "+stdout)
	}
	if len(parts) == 0 {
		return "no stdout or stderr output"
	}

	return strings.Join(parts, " | ")
}
