package agentrun

import "strings"

var reviewerLensValues = []string{
	"correctness",
	"test",
	"architecture",
	"security",
}

var findingSeverityValues = []string{
	"critical",
	"high",
	"medium",
	"low",
}

var findingConfidenceValues = []string{
	"high",
	"medium",
	"low",
}

// AllowedReviewerLenses returns the canonical reviewer lens values accepted by v1.
func AllowedReviewerLenses() []string {
	return append([]string(nil), reviewerLensValues...)
}

// NormalizeReviewerLens trims and lowercases a reviewer lens and reports whether it is supported.
func NormalizeReviewerLens(lens string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(lens)) {
	case "correctness", "test", "architecture", "security":
		return strings.ToLower(strings.TrimSpace(lens)), true
	default:
		return "", false
	}
}

// AllowedFindingSeverities returns the canonical reviewer finding severities accepted by v1.
func AllowedFindingSeverities() []string {
	return append([]string(nil), findingSeverityValues...)
}

// NormalizeFindingSeverity trims and lowercases a finding severity and reports whether it is supported.
func NormalizeFindingSeverity(severity string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(severity)), true
	default:
		return "", false
	}
}

// AllowedFindingConfidences returns the canonical reviewer finding confidences accepted by v1.
func AllowedFindingConfidences() []string {
	return append([]string(nil), findingConfidenceValues...)
}

// NormalizeFindingConfidence trims and lowercases a finding confidence and reports whether it is supported.
func NormalizeFindingConfidence(confidence string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(confidence)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(confidence)), true
	default:
		return "", false
	}
}
