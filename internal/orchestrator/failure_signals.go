package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"quick-ai-toolhub/internal/agentrun"
)

func stageFailureFingerprint(stage Stage, result StageResult) string {
	return buildFailureFingerprint(stage, result, "")
}

func reviewFailureFingerprint(_ int, findings []agentrun.Finding, fallback string) string {
	return buildFailureFingerprint(StageReview, StageResult{
		Stage:      StageReview,
		NextAction: fallback,
		Findings:   copyFindings(findings),
	}, fallback)
}

func buildFailureFingerprint(stage Stage, result StageResult, fallbackAction string) string {
	if failureFingerprint := strings.TrimSpace(result.FailureFingerprint); failureFingerprint != "" {
		return failureFingerprint
	}

	if failureFingerprint := findingsFailureFingerprint(stage, result.Findings); failureFingerprint != "" {
		return failureFingerprint
	}

	stageToken := normalizeFailureFingerprintToken(string(stage))
	statusToken := normalizeFailureFingerprintToken(result.Status)
	nextActionToken := normalizeFailureFingerprintToken(result.NextAction)
	if nextActionToken == "" {
		nextActionToken = normalizeFailureFingerprintToken(fallbackAction)
	}

	parts := make([]string, 0, 3)
	if stageToken != "" {
		parts = append(parts, stageToken)
	}
	if statusToken != "" {
		parts = append(parts, statusToken)
	}
	if nextActionToken != "" {
		parts = append(parts, nextActionToken)
	}
	if len(parts) == 0 {
		return "unknown_failure"
	}
	return strings.Join(parts, ":")
}

func findingsFailureFingerprint(stage Stage, findings []agentrun.Finding) string {
	fingerprints := normalizedFindingFingerprints(findings)
	switch len(fingerprints) {
	case 0:
		return ""
	case 1:
		return fingerprints[0]
	default:
		return fmt.Sprintf("%s:findings:%s", normalizeFailureFingerprintToken(string(stage)), hashFailureFingerprintParts(fingerprints))
	}
}

func normalizedFindingFingerprints(findings []agentrun.Finding) []string {
	if len(findings) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(findings))
	normalized := make([]string, 0, len(findings))
	for _, finding := range findings {
		fingerprint := strings.TrimSpace(finding.FindingFingerprint)
		if fingerprint == "" {
			continue
		}
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		normalized = append(normalized, fingerprint)
	}
	sort.Strings(normalized)
	return normalized
}

func hashFailureFingerprintParts(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:8])
}

func normalizeFailureFingerprintToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '/', '\\', ':':
			return true
		default:
			return false
		}
	}), "_")
}
