package reviewagg

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"quick-ai-toolhub/internal/agentrun"
)

var taskIDPattern = regexp.MustCompile(`^Sprint-[0-9]+/Task-[0-9]+$`)

type Service struct{}

type validationError struct {
	message string
}

func (e *validationError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func New() *Service {
	return &Service{}
}

func (s *Service) Name() string {
	return "review-result-tool"
}

func (s *Service) Execute(ctx context.Context, req Request) Response {
	data, err := s.execute(ctx, req)
	if err != nil {
		return Response{
			OK:    false,
			Error: asToolError(err),
		}
	}
	return Response{
		OK:   true,
		Data: &data,
	}
}

func (s *Service) execute(ctx context.Context, req Request) (ResponseData, error) {
	if ctx == nil {
		return ResponseData{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return ResponseData{}, err
	}

	normalized, err := normalizeRequest(req)
	if err != nil {
		return ResponseData{}, err
	}
	return buildResponse(normalized), nil
}

type normalizedRequest struct {
	TaskID       string
	SprintID     string
	ReviewResult ReviewResult
	Findings     []agentrun.Finding
}

func normalizeRequest(req Request) (normalizedRequest, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return normalizedRequest{}, newValidationError("task_id is required")
	}
	if !taskIDPattern.MatchString(taskID) {
		return normalizedRequest{}, newValidationError("task_id %q must match Sprint-<n>/Task-<n>", taskID)
	}
	derivedSprintID := deriveSprintID(taskID)
	sprintID := strings.TrimSpace(req.SprintID)
	if sprintID == "" {
		sprintID = derivedSprintID
	} else if sprintID != derivedSprintID {
		return normalizedRequest{}, newValidationError("sprint_id %q does not match task_id %q", sprintID, taskID)
	}

	reviewerID := strings.TrimSpace(req.ReviewResult.ReviewerID)
	if reviewerID == "" {
		return normalizedRequest{}, newValidationError("review_result.reviewer_id is required")
	}

	lens, ok := agentrun.NormalizeReviewerLens(req.ReviewResult.Lens)
	if !ok {
		return normalizedRequest{}, newValidationError("review_result.lens %q is invalid", strings.TrimSpace(req.ReviewResult.Lens))
	}

	status, err := normalizeReviewStatus(req.ReviewResult.Status)
	if err != nil {
		return normalizedRequest{}, newValidationError("review_result.status %q is invalid", strings.TrimSpace(req.ReviewResult.Status))
	}

	findings, err := normalizeFindings(req.ReviewResult.Findings, reviewerID, lens)
	if err != nil {
		return normalizedRequest{}, newValidationError("review_result: %s", err.Error())
	}
	if status == "request_changes" && len(findings) == 0 {
		return normalizedRequest{}, newValidationError("review_result.status %q requires at least one finding", status)
	}
	if status == "pass" && len(findings) > 0 {
		return normalizedRequest{}, newValidationError("review_result.status %q does not allow findings", status)
	}

	return normalizedRequest{
		TaskID:   taskID,
		SprintID: sprintID,
		ReviewResult: ReviewResult{
			ReviewerID: reviewerID,
			Lens:       lens,
			Status:     status,
			Findings:   findings,
		},
		Findings: findings,
	}, nil
}

func normalizeReviewStatus(raw string) (string, error) {
	switch status := strings.ToLower(strings.TrimSpace(raw)); status {
	case "pass", "request_changes", "awaiting_human":
		return status, nil
	default:
		return "", errors.New("unsupported status")
	}
}

func normalizeFindings(findings []agentrun.Finding, reviewerID, lens string) ([]agentrun.Finding, error) {
	if len(findings) == 0 {
		return []agentrun.Finding{}, nil
	}

	normalized := make([]agentrun.Finding, 0, len(findings))
	for index, finding := range findings {
		value, err := normalizeFinding(finding, reviewerID, lens)
		if err != nil {
			return nil, fmt.Errorf("findings[%d]: %w", index, err)
		}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func normalizeFinding(finding agentrun.Finding, reviewerID, lens string) (agentrun.Finding, error) {
	finding.ReviewerID = strings.TrimSpace(finding.ReviewerID)
	if finding.ReviewerID == "" {
		finding.ReviewerID = reviewerID
	}
	if finding.ReviewerID != reviewerID {
		return agentrun.Finding{}, errors.New("finding reviewer_id must match parent reviewer_id")
	}

	finding.Lens = strings.TrimSpace(finding.Lens)
	if finding.Lens == "" {
		finding.Lens = lens
	}
	normalizedLens, ok := agentrun.NormalizeReviewerLens(finding.Lens)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid lens %q", finding.Lens)
	}
	if normalizedLens != lens {
		return agentrun.Finding{}, errors.New("finding lens must match parent lens")
	}
	finding.Lens = normalizedLens

	finding.Severity, ok = agentrun.NormalizeFindingSeverity(finding.Severity)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid severity %q", strings.TrimSpace(finding.Severity))
	}
	finding.Confidence, ok = agentrun.NormalizeFindingConfidence(finding.Confidence)
	if !ok {
		return agentrun.Finding{}, fmt.Errorf("invalid confidence %q", strings.TrimSpace(finding.Confidence))
	}

	finding.Category = strings.TrimSpace(finding.Category)
	finding.Summary = strings.TrimSpace(finding.Summary)
	finding.Evidence = strings.TrimSpace(finding.Evidence)
	finding.FindingFingerprint = strings.TrimSpace(finding.FindingFingerprint)
	finding.SuggestedAction = strings.TrimSpace(finding.SuggestedAction)
	finding.FileRefs = dedupeOrderedStrings(finding.FileRefs)

	switch {
	case finding.Category == "":
		return agentrun.Finding{}, errors.New("missing category")
	case len(finding.FileRefs) == 0:
		return agentrun.Finding{}, errors.New("missing file_refs")
	case finding.Summary == "":
		return agentrun.Finding{}, errors.New("missing summary")
	case finding.Evidence == "":
		return agentrun.Finding{}, errors.New("missing evidence")
	case finding.FindingFingerprint == "":
		return agentrun.Finding{}, errors.New("missing finding_fingerprint")
	case finding.SuggestedAction == "":
		return agentrun.Finding{}, errors.New("missing suggested_action")
	default:
		return finding, nil
	}
}

func buildResponse(req normalizedRequest) ResponseData {
	reviewFindings := req.Findings
	if reviewFindings == nil {
		reviewFindings = []agentrun.Finding{}
	}

	hasCriticalFinding := false
	hasBlockingFinding := false
	for _, finding := range reviewFindings {
		if finding.Severity == "critical" {
			hasCriticalFinding = true
		}
		if finding.Severity == "critical" || finding.Severity == "high" {
			hasBlockingFinding = true
		}
	}

	hasReviewerEscalation := req.ReviewResult.Status == "awaiting_human"
	decision := DecisionPass
	switch {
	case hasReviewerEscalation:
		decision = DecisionAwaitingHuman
	case len(reviewFindings) > 0:
		decision = DecisionRequestChanges
	}

	return ResponseData{
		ReviewFindings:        reviewFindings,
		Decision:              decision,
		Summary:               buildSummary(decision, len(reviewFindings), hasCriticalFinding, hasBlockingFinding, hasReviewerEscalation),
		HasCriticalFinding:    hasCriticalFinding,
		HasBlockingFinding:    hasBlockingFinding,
		HasReviewerEscalation: hasReviewerEscalation,
	}
}

func buildSummary(decision Decision, findingCount int, hasCriticalFinding, hasBlockingFinding, hasReviewerEscalation bool) string {
	parts := []string{fmt.Sprintf("decision=%s", decision)}
	if findingCount == 0 {
		parts = append(parts, "no review findings")
	} else {
		parts = append(parts, fmt.Sprintf("review_findings=%d", findingCount))
	}
	if hasCriticalFinding {
		parts = append(parts, "critical finding present")
	}
	if hasBlockingFinding {
		parts = append(parts, "blocking finding present")
	}
	if hasReviewerEscalation {
		parts = append(parts, "reviewer escalation present")
	}
	return strings.Join(parts, "; ")
}

func dedupeOrderedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func deriveSprintID(taskID string) string {
	sprintID, _, _ := strings.Cut(taskID, "/")
	return sprintID
}

func newValidationError(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}

func asToolError(err error) *ToolError {
	if err == nil {
		return nil
	}
	var validationErr *validationError
	if errors.As(err, &validationErr) {
		return &ToolError{
			Code:      ErrorCodeInvalid,
			Message:   validationErr.message,
			Retryable: false,
		}
	}
	return &ToolError{
		Code:      ErrorCodeInternal,
		Message:   err.Error(),
		Retryable: false,
	}
}
