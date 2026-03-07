package tasklist

import "quick-ai-toolhub/internal/store"

type RefreshMode string

const (
	RefreshModeFull     RefreshMode = "full"
	RefreshModeTargeted RefreshMode = "targeted"

	ErrorCodeInvalid  = "invalid_request"
	ErrorCodeNotFound = "not_found"
	ErrorCodeInternal = "internal_failure"
)

type Request struct {
	RefreshMode RefreshMode `json:"refresh_mode"`
	SprintID    string      `json:"sprint_id,omitempty"`
}

type Response struct {
	OK    bool          `json:"ok"`
	Data  *ResponseData `json:"data,omitempty"`
	Error *ToolError    `json:"error,omitempty"`
}

type ResponseData struct {
	Sprints        []store.SprintProjection `json:"sprints"`
	Tasks          []TaskEntry              `json:"tasks"`
	SyncSummary    SyncSummary              `json:"sync_summary"`
	BlockingIssues []BlockingIssue          `json:"blocking_issues"`
}

type TaskEntry struct {
	Task      store.TaskProjection `json:"task"`
	BlockedBy []string             `json:"blocked_by"`
}

type SyncSummary struct {
	Mode        string `json:"mode"`
	RefreshedAt string `json:"refreshed_at"`
	SprintCount int    `json:"sprint_count"`
	TaskCount   int    `json:"task_count"`
}

type BlockingIssue struct {
	Scope    string `json:"scope"`
	EntityID string `json:"entity_id"`
	Reason   string `json:"reason"`
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
