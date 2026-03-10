package reviewagg

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"quick-ai-toolhub/internal/agentrun"
)

func TestExecutePassesSingleReviewerResult(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionPass || resp.Data.HasBlockingFinding || resp.Data.HasReviewerEscalation {
		t.Fatalf("unexpected response: %+v", resp.Data)
	}
	if len(resp.Data.ReviewFindings) != 0 {
		t.Fatalf("expected no review findings, got %+v", resp.Data.ReviewFindings)
	}
}

func TestServiceNameMatchesToolContract(t *testing.T) {
	t.Parallel()

	if name := New().Name(); name != "review-result-tool" {
		t.Fatalf("expected public tool name review-result-tool, got %q", name)
	}
}

func TestExecutePassSerializesEmptyReviewFindingsAsArray(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if !strings.Contains(string(payload), `"review_findings":[]`) {
		t.Fatalf("expected review_findings to serialize as [], got %s", payload)
	}
}

func TestExecuteRequestChangesReturnsNormalizedFindings(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "", "high", "medium", "blocking-finding"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionRequestChanges || !resp.Data.HasBlockingFinding {
		t.Fatalf("unexpected response: %+v", resp.Data)
	}
	if len(resp.Data.ReviewFindings) != 1 {
		t.Fatalf("expected one review finding, got %+v", resp.Data.ReviewFindings)
	}
	if got := resp.Data.ReviewFindings[0]; got.Lens != "correctness" || got.ReviewerID != "reviewer-correctness" {
		t.Fatalf("expected normalized reviewer metadata, got %+v", got)
	}
}

func TestExecuteAwaitingHumanPreservesEscalation(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-architecture",
			Lens:       "architecture",
			Status:     "awaiting_human",
			Findings: []agentrun.Finding{
				testFinding("reviewer-architecture", "architecture", "medium", "high", "needs-human"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionAwaitingHuman || !resp.Data.HasReviewerEscalation {
		t.Fatalf("unexpected response: %+v", resp.Data)
	}
}

func TestExecuteAwaitingHumanOverridesBlockingDecision(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-architecture",
			Lens:       "architecture",
			Status:     "awaiting_human",
			Findings: []agentrun.Finding{
				testFinding("reviewer-architecture", "architecture", "critical", "high", "needs-human-critical"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionAwaitingHuman {
		t.Fatalf("expected awaiting_human decision, got %+v", resp.Data)
	}
	if !resp.Data.HasReviewerEscalation || !resp.Data.HasCriticalFinding || !resp.Data.HasBlockingFinding {
		t.Fatalf("expected escalation and blocking metadata to be preserved, got %+v", resp.Data)
	}
}

func TestExecuteMediumFindingRequestsChangesWithoutBlockingMetadata(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "medium", "high", "non-blocking-medium"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionRequestChanges {
		t.Fatalf("expected request_changes decision, got %+v", resp.Data)
	}
	if resp.Data.HasCriticalFinding || resp.Data.HasBlockingFinding || resp.Data.HasReviewerEscalation {
		t.Fatalf("expected non-blocking metadata only, got %+v", resp.Data)
	}
}

func TestExecuteAwaitingHumanAllowsEmptyFindings(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-architecture",
			Lens:       "architecture",
			Status:     "awaiting_human",
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionAwaitingHuman || !resp.Data.HasReviewerEscalation {
		t.Fatalf("expected awaiting_human escalation without findings, got %+v", resp.Data)
	}
	if len(resp.Data.ReviewFindings) != 0 || resp.Data.HasCriticalFinding || resp.Data.HasBlockingFinding {
		t.Fatalf("expected no findings or blocking metadata, got %+v", resp.Data)
	}
}

func TestExecutePreservesRepeatedFindingFingerprints(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "medium", "low", "duplicate-finding"),
				func() agentrun.Finding {
					finding := testFinding("reviewer-correctness", "correctness", "medium", "low", "duplicate-finding")
					finding.FileRefs = []string{"internal/reviewagg/service.go", "internal/reviewagg/model.go"}
					return finding
				}(),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if len(resp.Data.ReviewFindings) != 2 {
		t.Fatalf("expected duplicate findings to be preserved, got %+v", resp.Data.ReviewFindings)
	}
	if got := resp.Data.ReviewFindings[0]; len(got.FileRefs) != 1 {
		t.Fatalf("expected original finding payload to remain unchanged, got %+v", got)
	}
}

func TestExecuteDedupesRepeatedFingerprintsUsingStrongestSeverityAndConfidence(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "medium", "low", "duplicate-stronger"),
				testFinding("reviewer-correctness", "correctness", "critical", "high", "duplicate-stronger"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if len(resp.Data.ReviewFindings) != 2 {
		t.Fatalf("expected duplicate findings to be preserved, got %+v", resp.Data.ReviewFindings)
	}
	got := resp.Data.ReviewFindings[1]
	if got.Severity != "critical" || got.Confidence != "high" {
		t.Fatalf("expected critical duplicate copy to remain present, got %+v", got)
	}
	if !resp.Data.HasCriticalFinding || !resp.Data.HasBlockingFinding || resp.Data.Decision != DecisionRequestChanges {
		t.Fatalf("expected critical blocking request_changes response, got %+v", resp.Data)
	}
}

func TestExecuteRejectsRequestChangesWithoutFindings(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsPassWithFindings(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "low", "high", "pass-with-findings"),
			},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteDerivesSprintIDWhenOmitted(t *testing.T) {
	t.Parallel()

	normalized, err := normalizeRequest(Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})
	if err != nil {
		t.Fatalf("normalize request: %v", err)
	}
	if normalized.SprintID != "Sprint-04" {
		t.Fatalf("expected derived sprint_id, got %+v", normalized)
	}
}

func TestExecuteRejectsMismatchedSprintID(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID:   "Sprint-04/Task-03",
		SprintID: "Sprint-99",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "completed_with_findings",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsInvalidTaskID(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsMissingReviewerID(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "   ",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsInvalidLens(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "performance",
			Status:     "pass",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsFindingReviewerMismatch(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-security", "correctness", "high", "medium", "mismatch"),
			},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsFindingLensMismatch(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "security", "high", "medium", "lens-mismatch"),
			},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsFindingWithoutFileRefs(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "missing-file-refs")
	finding.FileRefs = []string{" ", ""}

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsInvalidFindingSeverity(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "urgent", "medium", "invalid-severity"),
			},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteRejectsInvalidFindingConfidence(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "high", "certain", "invalid-confidence"),
			},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid response, got %+v", resp)
	}
}

func TestExecuteCriticalFindingBlocksWithRequestChanges(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "critical", "high", "critical-bug"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionRequestChanges {
		t.Fatalf("expected request_changes decision for critical finding, got %s", resp.Data.Decision)
	}
	if !resp.Data.HasCriticalFinding {
		t.Fatal("expected has_critical_finding=true for critical severity")
	}
	if !resp.Data.HasBlockingFinding {
		t.Fatal("expected has_blocking_finding=true for critical severity")
	}
	if resp.Data.HasReviewerEscalation {
		t.Fatal("expected has_reviewer_escalation=false for request_changes status")
	}
}

func TestExecuteLowSeverityFindingTriggersRequestChanges(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "low", "low", "minor-issue"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionRequestChanges {
		t.Fatalf("expected request_changes for low severity finding, got %s", resp.Data.Decision)
	}
	if resp.Data.HasCriticalFinding || resp.Data.HasBlockingFinding {
		t.Fatal("low severity should not trigger critical or blocking flags")
	}
}

func TestExecuteRejectsEmptyTaskID(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for empty task_id, got %+v", resp)
	}
}

func TestExecuteRejectsNilContext(t *testing.T) {
	t.Parallel()

	resp := New().Execute(nil, Request{ //nolint:staticcheck
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "pass",
		},
	})

	if resp.OK {
		t.Fatal("expected error for nil context")
	}
}

func TestExecuteRejectsFindingMissingCategory(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "no-category")
	finding.Category = ""

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for missing category, got %+v", resp)
	}
}

func TestExecuteRejectsFindingMissingSummary(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "no-summary")
	finding.Summary = ""

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for missing summary, got %+v", resp)
	}
}

func TestExecuteRejectsFindingMissingEvidence(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "no-evidence")
	finding.Evidence = ""

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for missing evidence, got %+v", resp)
	}
}

func TestExecuteRejectsFindingMissingFingerprint(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "")
	finding.FindingFingerprint = ""

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for missing fingerprint, got %+v", resp)
	}
}

func TestExecuteRejectsFindingMissingSuggestedAction(t *testing.T) {
	t.Parallel()

	finding := testFinding("reviewer-correctness", "correctness", "medium", "medium", "no-action")
	finding.SuggestedAction = ""

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings:   []agentrun.Finding{finding},
		},
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorCodeInvalid {
		t.Fatalf("expected invalid_request for missing suggested_action, got %+v", resp)
	}
}

func TestExecuteHighSeverityTriggersBlockingButNotCritical(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "high", "high", "high-sev-issue"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionRequestChanges {
		t.Fatalf("expected request_changes, got %s", resp.Data.Decision)
	}
	if resp.Data.HasCriticalFinding {
		t.Fatal("high severity should not trigger has_critical_finding")
	}
	if !resp.Data.HasBlockingFinding {
		t.Fatal("high severity should trigger has_blocking_finding")
	}
}

func TestExecuteDecisionPriorityEscalationOverridesBlocking(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-security",
			Lens:       "security",
			Status:     "awaiting_human",
			Findings: []agentrun.Finding{
				testFinding("reviewer-security", "security", "high", "high", "security-escalation"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Data.Decision != DecisionAwaitingHuman {
		t.Fatalf("escalation should override blocking: expected awaiting_human, got %s", resp.Data.Decision)
	}
	if !resp.Data.HasBlockingFinding || !resp.Data.HasReviewerEscalation {
		t.Fatal("both blocking and escalation metadata should be preserved")
	}
}

func TestExecuteResponseDataFieldsAreStructured(t *testing.T) {
	t.Parallel()

	resp := New().Execute(context.Background(), Request{
		TaskID: "Sprint-04/Task-03",
		ReviewResult: ReviewResult{
			ReviewerID: "reviewer-correctness",
			Lens:       "correctness",
			Status:     "request_changes",
			Findings: []agentrun.Finding{
				testFinding("reviewer-correctness", "correctness", "critical", "high", "struct-test"),
			},
		},
	})

	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	data := resp.Data
	if data.Decision == "" {
		t.Fatal("decision field must be non-empty")
	}
	if data.Summary == "" {
		t.Fatal("summary field must be non-empty")
	}
	// Callers must not need to parse summary to determine escalation/blocking
	if !data.HasCriticalFinding {
		t.Fatal("has_critical_finding must be true for critical severity")
	}
	if !data.HasBlockingFinding {
		t.Fatal("has_blocking_finding must be true for critical severity")
	}
}

func TestExecuteAllLensValuesAccepted(t *testing.T) {
	t.Parallel()

	for _, lens := range []string{"correctness", "test", "architecture", "security"} {
		resp := New().Execute(context.Background(), Request{
			TaskID: "Sprint-04/Task-03",
			ReviewResult: ReviewResult{
				ReviewerID: "reviewer-" + lens,
				Lens:       lens,
				Status:     "pass",
			},
		})

		if !resp.OK {
			t.Fatalf("expected pass for lens %q, got error: %+v", lens, resp.Error)
		}
	}
}

func testFinding(reviewerID, lens, severity, confidence, fingerprint string) agentrun.Finding {
	return agentrun.Finding{
		ReviewerID:         reviewerID,
		Lens:               lens,
		Severity:           severity,
		Confidence:         confidence,
		Category:           "correctness",
		FileRefs:           []string{"internal/reviewagg/service.go"},
		Summary:            "review finding",
		Evidence:           "structured reviewer evidence",
		FindingFingerprint: fingerprint,
		SuggestedAction:    "fix the issue",
	}
}
