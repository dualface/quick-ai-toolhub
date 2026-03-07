package githubsync

import (
	"encoding/json"
	"fmt"
)

type Operation string

const (
	OpFullReconcile     Operation = "full_reconcile"
	OpIngestWebhook     Operation = "ingest_webhook"
	OpReconcileIssue    Operation = "reconcile_issue"
	OpReconcilePullReq  Operation = "reconcile_pull_request"
	OpReconcileCIRun    Operation = "reconcile_ci_run"
	ErrorCodeInvalid    string    = "invalid_request"
	ErrorCodeGitHubRead string    = "github_read_failed"
	ErrorCodeGitHubData string    = "github_data_invalid"
	ErrorCodeProjection string    = "projection_write_failed"
	ErrorCodeInternal   string    = "internal_failure"
)

type Request struct {
	Op      Operation       `json:"op"`
	Payload json.RawMessage `json:"payload"`
}

type FullReconcilePayload struct {
	Reason string `json:"reason"`
}

type Response struct {
	OK    bool          `json:"ok"`
	Data  *ResponseData `json:"data,omitempty"`
	Error *ToolError    `json:"error,omitempty"`
}

type ResponseData struct {
	SyncSummary     SyncSummary     `json:"sync_summary"`
	ChangedEntities []ChangedEntity `json:"changed_entities,omitempty"`
}

type SyncSummary struct {
	Op           string `json:"op"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	ChangedCount int    `json:"changed_count"`
}

type ChangedEntity struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
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
	Repo          string
	DefaultBranch string
}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	return e.message
}

func newValidationError(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}
