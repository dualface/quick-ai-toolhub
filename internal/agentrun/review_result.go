package agentrun

import (
	"errors"
	"strings"
)

var (
	reviewerPassStatusAliases = map[string]struct{}{
		"approve":             {},
		"completed":           {},
		"pass":                {},
		"passed":              {},
		"passed_with_warning": {},
		"success":             {},
	}
	reviewerRequestChangesAliases = map[string]struct{}{
		"completed_with_findings": {},
		"findings_detected":       {},
		"needs_changes":           {},
		"needs_fix":               {},
		"pass_with_findings":      {},
		"request_changes":         {},
	}
	reviewerAwaitingHumanAliases = map[string]struct{}{
		"awaiting_human": {},
		"blocked":        {},
		"fail":           {},
		"failed":         {},
		"timeout":        {},
	}
)

func normalizeReviewerResult(result Result) (Result, error) {
	status, err := normalizeReviewerResultStatus(result.Status)
	if err != nil {
		return Result{}, err
	}

	findings := result.Findings
	if status == "request_changes" && len(findings) == 0 {
		return Result{}, errors.New("review_result.status \"request_changes\" requires at least one finding")
	}
	if status == "pass" && len(findings) > 0 {
		return Result{}, errors.New("review_result.status \"pass\" does not allow findings")
	}

	result.Status = status
	result.Findings = findings
	return result, nil
}

func normalizeReviewerResultStatus(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case hasReviewerStatusAlias(reviewerPassStatusAliases, status):
		return "pass", nil
	case hasReviewerStatusAlias(reviewerRequestChangesAliases, status):
		return "request_changes", nil
	case hasReviewerStatusAlias(reviewerAwaitingHumanAliases, status):
		return "awaiting_human", nil
	default:
		return "", errors.New("unsupported reviewer status")
	}
}

func hasReviewerStatusAlias(values map[string]struct{}, status string) bool {
	_, ok := values[status]
	return ok
}
