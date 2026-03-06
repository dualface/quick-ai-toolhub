package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

const ExitCodeUnavailable = -1

type Request struct {
	WorkDir      string
	Args         []string
	Stdin        []byte
	Env          []string
	StdoutWriter io.Writer
	StderrWriter io.Writer
	Timeout      time.Duration
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	TimedOut bool
	Duration time.Duration
	Err      error
}

type Executor struct{}

func (Executor) Run(ctx context.Context, req Request) Result {
	startedAt := time.Now()
	result := Result{ExitCode: ExitCodeUnavailable}

	if ctx == nil {
		result.Err = errors.New("nil context")
		return result
	}
	if err := ctx.Err(); err != nil {
		result.Err = err
		return result
	}
	if len(req.Args) == 0 {
		result.Err = errors.New("missing command")
		return result
	}

	runCtx := ctx
	cancel := func() {}
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Args[0], req.Args[1:]...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = bytes.NewReader(req.Stdin)
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = outputWriter(&stdout, req.StdoutWriter)
	cmd.Stderr = outputWriter(&stderr, req.StderrWriter)

	err := cmd.Run()
	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	result.Duration = time.Since(startedAt)
	result.ExitCode = exitCode(err, cmd.ProcessState)

	if runErr := runCtx.Err(); runErr != nil {
		result.Err = runErr
		result.TimedOut = errors.Is(runErr, context.DeadlineExceeded)
		return result
	}

	result.Err = err
	return result
}

func outputWriter(buffer *bytes.Buffer, extra io.Writer) io.Writer {
	if extra == nil {
		return buffer
	}
	return io.MultiWriter(buffer, extra)
}

func exitCode(err error, state *os.ProcessState) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if state != nil {
		return state.ExitCode()
	}
	return ExitCodeUnavailable
}
