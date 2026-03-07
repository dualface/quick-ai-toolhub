package agentrun

import (
	"context"
	"io"
	"time"
)

type ExecuteOptions struct {
	PlanFile          string
	TasksDir          string
	WorkDir           string
	OutputRoot        string
	Timeout           time.Duration
	Yolo              bool
	IsolatedCodexHome bool
	StreamOutput      io.Writer
	ProgressOutput    io.Writer
}

func (e *Executor) Execute(ctx context.Context, req Request, opts ExecuteOptions) Response {
	if req.TimeoutSeconds < 0 {
		return Response{
			OK: false,
			Error: &ToolError{
				Code:      ErrorCodeInvalidRequest,
				Message:   "timeout_seconds must be non-negative",
				Retryable: false,
			},
		}
	}

	runOpts := RunOptions{
		TaskID:            req.TaskID,
		AgentType:         req.AgentType,
		Attempt:           req.Attempt,
		Lens:              req.Lens,
		ContextRefs:       req.ContextRefs,
		ConfigFile:        req.ConfigFile,
		PlanFile:          opts.PlanFile,
		TasksDir:          opts.TasksDir,
		WorkDir:           opts.WorkDir,
		OutputRoot:        opts.OutputRoot,
		Model:             req.Model,
		Yolo:              opts.Yolo,
		IsolatedCodexHome: opts.IsolatedCodexHome,
		Timeout:           opts.Timeout,
		StreamOutput:      opts.StreamOutput,
		ProgressOutput:    opts.ProgressOutput,
	}
	if req.TimeoutSeconds > 0 {
		runOpts.Timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}

	result, err := e.RunTask(ctx, runOpts)
	if err != nil {
		return Response{
			OK:    false,
			Error: AsToolError(err),
		}
	}

	return Response{
		OK:   true,
		Data: &result,
	}
}
