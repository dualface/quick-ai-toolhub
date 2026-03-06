package agentrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type CommandRequest struct {
	WorkDir string
	Args    []string
	Stdin   []byte
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
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if err != nil {
		return result, fmt.Errorf("%s: %w: %s", strings.Join(req.Args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return result, nil
}
