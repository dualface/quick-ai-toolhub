package githubsync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	toolgithub "quick-ai-toolhub/internal/github"
	"quick-ai-toolhub/internal/store"
)

const (
	gitHubDeliveryHeader = "X-GitHub-Delivery"
	gitHubEventHeader    = "X-GitHub-Event"
	webhookEntityID      = "github_webhook"
)

type webhookReconcile struct {
	Op      Operation
	Payload any
	Reason  string
}

type webhookAction struct {
	EventType  string
	EntityType string
	EntityID   string
	SprintID   *string
	TaskID     *string
	OccurredAt string
	Reconcile  *webhookReconcile
}

type webhookIssueEnvelope struct {
	Action string       `json:"action"`
	Issue  webhookIssue `json:"issue"`
}

type webhookIssue struct {
	Number    int            `json:"number"`
	NodeID    string         `json:"node_id"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	State     string         `json:"state"`
	URL       string         `json:"html_url"`
	Labels    []webhookLabel `json:"labels"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	ClosedAt  string         `json:"closed_at"`
}

type webhookLabel struct {
	Name string `json:"name"`
}

type webhookPullRequestEnvelope struct {
	Action      string             `json:"action"`
	Number      int                `json:"number"`
	PullRequest webhookPullRequest `json:"pull_request"`
}

type webhookPullRequest struct {
	Number    int                `json:"number"`
	NodeID    string             `json:"node_id"`
	State     string             `json:"state"`
	URL       string             `json:"html_url"`
	Head      webhookPRBranchRef `json:"head"`
	Base      webhookPRBranchRef `json:"base"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
	ClosedAt  string             `json:"closed_at"`
	MergedAt  string             `json:"merged_at"`
}

type webhookPRBranchRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type webhookWorkflowRunEnvelope struct {
	Action      string             `json:"action"`
	WorkflowRun webhookWorkflowRun `json:"workflow_run"`
}

type webhookWorkflowRun struct {
	ID           int64  `json:"id"`
	HeadBranch   string `json:"head_branch"`
	HeadSHA      string `json:"head_sha"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	URL          string `json:"html_url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	RunStartedAt string `json:"run_started_at"`
}

func validateIngestWebhookPayload(payload IngestWebhookPayload) error {
	if strings.TrimSpace(payload.DeliveryID) == "" {
		return newValidationError("ingest_webhook.delivery_id is required")
	}
	if strings.TrimSpace(payload.EventName) == "" {
		return newValidationError("ingest_webhook.event_name is required")
	}
	return nil
}

func decodeWebhookAction(payload IngestWebhookPayload) (webhookAction, error) {
	eventName := strings.ToLower(strings.TrimSpace(payload.EventName))
	raw, err := json.Marshal(payload.PayloadJSON)
	if err != nil {
		return webhookAction{}, newValidationError("marshal ingest_webhook.payload_json: %v", err)
	}

	base := webhookAction{
		EventType:  "github.webhook." + eventName,
		EntityType: "system",
		EntityID:   webhookEntityID,
		OccurredAt: currentUTCTimestamp(),
	}

	switch eventName {
	case "issues":
		var envelope webhookIssueEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return webhookAction{}, newValidationError("decode issues webhook payload: %v", err)
		}
		if envelope.Issue.Number <= 0 {
			return webhookAction{}, newValidationError("issues webhook payload is missing issue.number")
		}
		base.OccurredAt = webhookIssueOccurredAt(envelope.Issue)
		entityType, entityID, sprintID, taskID := webhookEntityForIssue(envelope.Issue)
		base.EntityType = entityType
		base.EntityID = entityID
		base.SprintID = sprintID
		base.TaskID = taskID
		base.Reconcile = &webhookReconcile{
			Op:      OpReconcileIssue,
			Payload: ReconcileIssuePayload{GitHubIssueNumber: envelope.Issue.Number},
			Reason:  "webhook_issues",
		}
		return base, nil
	case "pull_request":
		var envelope webhookPullRequestEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return webhookAction{}, newValidationError("decode pull_request webhook payload: %v", err)
		}
		number := envelope.Number
		if envelope.PullRequest.Number > 0 {
			number = envelope.PullRequest.Number
		}
		if number <= 0 {
			return webhookAction{}, newValidationError("pull_request webhook payload is missing number")
		}
		base.OccurredAt = webhookPullRequestOccurredAt(envelope.PullRequest)
		entityType, entityID, sprintID, taskID := webhookEntityForHeadBranch(envelope.PullRequest.Head.Ref)
		base.EntityType = entityType
		base.EntityID = entityID
		base.SprintID = sprintID
		base.TaskID = taskID
		base.Reconcile = &webhookReconcile{
			Op:      OpReconcilePullReq,
			Payload: ReconcilePullRequestPayload{GitHubPRNumber: number},
			Reason:  "webhook_pull_request",
		}
		return base, nil
	case "workflow_run":
		var envelope webhookWorkflowRunEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return webhookAction{}, newValidationError("decode workflow_run webhook payload: %v", err)
		}
		if envelope.WorkflowRun.ID <= 0 {
			return webhookAction{}, newValidationError("workflow_run webhook payload is missing workflow_run.id")
		}
		base.OccurredAt = webhookWorkflowRunOccurredAt(envelope.WorkflowRun)
		entityType, entityID, sprintID, taskID := webhookEntityForHeadBranch(envelope.WorkflowRun.HeadBranch)
		base.EntityType = entityType
		base.EntityID = entityID
		base.SprintID = sprintID
		base.TaskID = taskID
		base.Reconcile = &webhookReconcile{
			Op:      OpReconcileCIRun,
			Payload: ReconcileCIRunPayload{GitHubRunID: envelope.WorkflowRun.ID},
			Reason:  "webhook_workflow_run",
		}
		return base, nil
	default:
		return base, nil
	}
}

func webhookEventPayload(payload IngestWebhookPayload, action webhookAction) store.AppendEventPayload {
	eventPayload := map[string]any{
		"delivery_id": payload.DeliveryID,
		"event_name":  strings.ToLower(strings.TrimSpace(payload.EventName)),
		"payload":     payload.PayloadJSON,
	}

	return store.AppendEventPayload{
		EventID:        "evt_github_webhook_" + strings.TrimSpace(payload.DeliveryID),
		EntityType:     action.EntityType,
		EntityID:       action.EntityID,
		SprintID:       action.SprintID,
		TaskID:         action.TaskID,
		EventType:      action.EventType,
		Source:         "github_webhook",
		IdempotencyKey: "github_webhook:" + strings.TrimSpace(payload.DeliveryID),
		PayloadJSON:    eventPayload,
		OccurredAt:     action.OccurredAt,
	}
}

func webhookEntityForIssue(issue webhookIssue) (string, string, *string, *string) {
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if trimmed := strings.TrimSpace(label.Name); trimmed != "" {
			labels = append(labels, trimmed)
		}
	}

	normalized := toolgithub.Issue{
		GitHubIssueNumber: issue.Number,
		GitHubIssueNodeID: strings.TrimSpace(issue.NodeID),
		Title:             issue.Title,
		Body:              issue.Body,
		State:             strings.ToLower(strings.TrimSpace(issue.State)),
		URL:               issue.URL,
		Labels:            labels,
		CreatedAt:         issue.CreatedAt,
		UpdatedAt:         issue.UpdatedAt,
		ClosedAt:          strings.TrimSpace(issue.ClosedAt),
	}

	if parsed, err := parseSprintIssue(normalized); err == nil {
		return "sprint", parsed.Snapshot.SprintID, optionalText(parsed.Snapshot.SprintID), nil
	}
	if parsed, err := parseTaskIssue(normalized, "", 0); err == nil {
		return "task", parsed.Snapshot.TaskID, optionalText(parsed.Snapshot.SprintID), optionalText(parsed.Snapshot.TaskID)
	}
	return "system", webhookEntityID, nil, nil
}

func webhookEntityForHeadBranch(headBranch string) (string, string, *string, *string) {
	if sprintID, taskID, ok := parseTaskBranch(headBranch); ok {
		return "task", taskID, optionalText(sprintID), optionalText(taskID)
	}
	if sprintID, ok := parseSprintBranch(headBranch); ok {
		return "sprint", sprintID, optionalText(sprintID), nil
	}
	return "system", webhookEntityID, nil, nil
}

func webhookIssueOccurredAt(issue webhookIssue) string {
	for _, value := range []string{issue.UpdatedAt, issue.ClosedAt, issue.CreatedAt} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return currentUTCTimestamp()
}

func webhookPullRequestOccurredAt(pr webhookPullRequest) string {
	for _, value := range []string{pr.UpdatedAt, pr.MergedAt, pr.ClosedAt, pr.CreatedAt} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return currentUTCTimestamp()
}

func webhookWorkflowRunOccurredAt(run webhookWorkflowRun) string {
	for _, value := range []string{run.UpdatedAt, run.RunStartedAt, run.CreatedAt} {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return currentUTCTimestamp()
}

func ParseWebhookRequest(r *http.Request) (IngestWebhookPayload, error) {
	if r == nil {
		return IngestWebhookPayload{}, newValidationError("webhook request is required")
	}
	if r.Method != http.MethodPost {
		return IngestWebhookPayload{}, newValidationError("webhook request method must be POST")
	}

	deliveryID := strings.TrimSpace(r.Header.Get(gitHubDeliveryHeader))
	if deliveryID == "" {
		return IngestWebhookPayload{}, newValidationError("%s header is required", gitHubDeliveryHeader)
	}

	eventName := strings.TrimSpace(r.Header.Get(gitHubEventHeader))
	if eventName == "" {
		return IngestWebhookPayload{}, newValidationError("%s header is required", gitHubEventHeader)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return IngestWebhookPayload{}, newValidationError("read webhook request body: %v", err)
	}

	payloadJSON := map[string]any{}
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		if err := json.Unmarshal(body, &payloadJSON); err != nil {
			return IngestWebhookPayload{}, newValidationError("decode webhook request body: %v", err)
		}
	}

	return IngestWebhookPayload{
		DeliveryID:  deliveryID,
		EventName:   eventName,
		PayloadJSON: payloadJSON,
	}, nil
}

type WebhookHandler struct {
	service *Service
	opts    ExecuteOptions
}

func NewWebhookHandler(service *Service, opts ExecuteOptions) http.Handler {
	return WebhookHandler{
		service: service,
		opts:    opts,
	}
}

func (h WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	response := Response{
		OK: false,
		Error: &ToolError{
			Code:      ErrorCodeInternal,
			Message:   "githubsync service is nil",
			Retryable: false,
		},
	}
	statusCode := http.StatusInternalServerError

	if h.service != nil {
		payload, err := ParseWebhookRequest(r)
		if err != nil {
			response = Response{
				OK: false,
				Error: &ToolError{
					Code:      ErrorCodeInvalid,
					Message:   err.Error(),
					Retryable: false,
				},
			}
			statusCode = http.StatusBadRequest
		} else {
			response = h.service.Execute(r.Context(), Request{
				Op:      OpIngestWebhook,
				Payload: mustMarshalWebhookPayload(payload),
			}, h.opts)
			statusCode = statusCodeForWebhookResponse(response)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response)
}

func mustMarshalWebhookPayload(payload IngestWebhookPayload) json.RawMessage {
	data, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("marshal webhook payload: %v", err))
	}
	return data
}

func statusCodeForWebhookResponse(response Response) int {
	if response.OK {
		return http.StatusOK
	}
	if response.Error == nil {
		return http.StatusInternalServerError
	}
	if response.Error.Code == ErrorCodeInvalid {
		return http.StatusBadRequest
	}
	if response.Error.Code == ErrorCodeGitHubRead {
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}
