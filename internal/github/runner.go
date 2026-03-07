package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sharedcommand "quick-ai-toolhub/internal/command"
)

type Runner interface {
	Run(ctx context.Context, workdir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("missing command")
	}

	result := sharedcommand.Executor{}.Run(ctx, sharedcommand.Request{
		WorkDir: workdir,
		Args:    args,
	})
	if result.Err != nil {
		return result.Stdout, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), result.Err, strings.TrimSpace(string(result.Stderr)))
	}

	return result.Stdout, nil
}
