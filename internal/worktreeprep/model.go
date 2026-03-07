package worktreeprep

import "fmt"

const (
	ErrorCodeInvalid  = "invalid_request"
	ErrorCodeConflict = "conflict"
	ErrorCodeGit      = "git_failed"
	ErrorCodeInternal = "internal_failure"
)

type Request struct {
	SprintID     string `json:"sprint_id"`
	TaskID       string `json:"task_id"`
	SprintBranch string `json:"sprint_branch"`
	TaskBranch   string `json:"task_branch"`
	WorktreeRoot string `json:"worktree_root,omitempty"`
}

type Response struct {
	OK    bool          `json:"ok"`
	Data  *ResponseData `json:"data,omitempty"`
	Error *ToolError    `json:"error,omitempty"`
}

type ResponseData struct {
	WorktreePath  string `json:"worktree_path"`
	TaskBranch    string `json:"task_branch"`
	BaseBranch    string `json:"base_branch"`
	BaseCommitSHA string `json:"base_commit_sha"`
	Reused        bool   `json:"reused"`
}

type ToolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ExecuteOptions struct {
	WorkDir       string
	DefaultBranch string
	Remote        string
}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

type conflictError struct {
	message string
}

func (e *conflictError) Error() string {
	return e.message
}

func newValidationError(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}

func newConflictError(format string, args ...any) error {
	return &conflictError{message: fmt.Sprintf(format, args...)}
}
