package agentrun

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ErrorCodeInvalidRequest      = "invalid_request"
	ErrorCodePlanLoadFailed      = "plan_load_failed"
	ErrorCodeTaskNotFound        = "task_not_found"
	ErrorCodeInternalFailure     = "internal_failure"
	ErrorCodeRunnerExecution     = "runner_execution_failed"
	ErrorCodeRunnerTimeout       = "runner_timeout"
	ErrorCodeMalformedOutput     = "malformed_output"
	ErrorCodeSchemaBuildFailed   = "schema_build_failed"
	ErrorCodePromptBuildFailed   = "prompt_build_failed"
	ErrorCodeArtifactWriteFailed = "artifact_write_failed"
)

type ToolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *ToolError) Error() string {
	return e.Message
}

func newToolError(code, message string, retryable bool) *ToolError {
	return &ToolError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
	}
}

func wrapToolError(code string, retryable bool, format string, args ...any) *ToolError {
	return newToolError(code, fmt.Sprintf(format, args...), retryable)
}

func AsToolError(err error) *ToolError {
	if err == nil {
		return nil
	}

	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "malformed_output"):
		return newToolError(ErrorCodeMalformedOutput, msg, true)
	case strings.Contains(msg, "timeout"):
		return newToolError(ErrorCodeRunnerTimeout, msg, true)
	default:
		return newToolError(ErrorCodeInternalFailure, msg, false)
	}
}
