package reviewagg

import "quick-ai-toolhub/internal/agentrun"

const (
	ErrorCodeInvalid  = "invalid_request"
	ErrorCodeInternal = "internal_failure"
)

type Decision string

const (
	DecisionPass           Decision = "pass"
	DecisionRequestChanges Decision = "request_changes"
	DecisionAwaitingHuman  Decision = "awaiting_human"
)

type Request struct {
	TaskID       string       `json:"task_id"`
	SprintID     string       `json:"sprint_id,omitempty"`
	ReviewResult ReviewResult `json:"review_result"`
}

type ReviewResult struct {
	ReviewerID string             `json:"reviewer_id"`
	Lens       string             `json:"lens"`
	Status     string             `json:"status"`
	Findings   []agentrun.Finding `json:"findings"`
}

type Response struct {
	OK    bool          `json:"ok"`
	Data  *ResponseData `json:"data,omitempty"`
	Error *ToolError    `json:"error,omitempty"`
}

type ResponseData struct {
	ReviewFindings        []agentrun.Finding `json:"review_findings"`
	Decision              Decision           `json:"decision"`
	Summary               string             `json:"summary"`
	HasCriticalFinding    bool               `json:"has_critical_finding"`
	HasBlockingFinding    bool               `json:"has_blocking_finding"`
	HasReviewerEscalation bool               `json:"has_reviewer_escalation"`
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
