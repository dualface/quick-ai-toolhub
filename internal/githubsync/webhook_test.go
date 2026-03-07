package githubsync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookHandlerReceivesIssueWebhook(t *testing.T) {
	reader := newIncrementalReader()
	baseStore, storeService := openProjectionStore(t)

	runGitHubSync(t, baseStore, reader, Request{
		Op:      OpFullReconcile,
		Payload: mustJSON(t, FullReconcilePayload{Reason: "manual"}),
	})

	updatedTask := taskIssue(201, "Sprint-02", "Task-01", "Implement incremental github-sync-tool")
	updatedTask.Labels = []string{"kind/task", "needs-human"}
	updatedTask.UpdatedAt = "2026-03-07T06:00:00Z"
	reader.openTaskIssues[0] = updatedTask
	reader.issues[201] = updatedTask

	body, err := json.Marshal(map[string]any{
		"action": "edited",
		"issue": map[string]any{
			"number":     updatedTask.GitHubIssueNumber,
			"node_id":    updatedTask.GitHubIssueNodeID,
			"title":      updatedTask.Title,
			"body":       updatedTask.Body,
			"state":      updatedTask.State,
			"html_url":   updatedTask.URL,
			"created_at": updatedTask.CreatedAt,
			"updated_at": updatedTask.UpdatedAt,
			"labels": []map[string]any{
				{"name": "kind/task"},
				{"name": "needs-human"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal webhook body: %v", err)
	}

	handler := NewWebhookHandler(New(Dependencies{
		GitHub: reader,
		Store:  baseStore,
	}), ExecuteOptions{
		WorkDir:       t.TempDir(),
		Repo:          "acme/quick-ai-toolhub",
		DefaultBranch: "main",
	})

	req := httptest.NewRequest(http.MethodPost, "/github/webhook", bytes.NewReader(body))
	req.Header.Set(gitHubDeliveryHeader, "delivery-http-1")
	req.Header.Set(gitHubEventHeader, "issues")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d body=%s", recorder.Code, recorder.Body.String())
	}

	var response Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.OK {
		t.Fatalf("expected webhook handler response to succeed, got %#v", response.Error)
	}

	db, err := storeService.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM events WHERE idempotency_key = 'github_webhook:delivery-http-1'`, 1)

	var needsHuman int
	if err := db.QueryRowContext(context.Background(), `SELECT needs_human FROM tasks WHERE task_id = ?`, "Sprint-02/Task-01").Scan(&needsHuman); err != nil {
		t.Fatalf("load task needs_human: %v", err)
	}
	if needsHuman != 1 {
		t.Fatalf("expected task needs_human=1 after webhook, got %d", needsHuman)
	}
}

func TestParseWebhookRequestRejectsMissingHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/github/webhook", bytes.NewReader([]byte(`{}`)))
	if _, err := ParseWebhookRequest(req); err == nil {
		t.Fatal("expected missing header validation error")
	}
}
