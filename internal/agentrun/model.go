package agentrun

import (
	"encoding/json"
	"io"
	"time"
)

type RunnerID string

const (
	RunnerCodexExec RunnerID = "codex_exec"
)

type AgentType string

const (
	AgentDeveloper AgentType = "developer"
	AgentQA        AgentType = "qa"
	AgentReviewer  AgentType = "reviewer"
)

type Request struct {
	AgentType      AgentType   `json:"agent_type"`
	TaskID         string      `json:"task_id"`
	Attempt        int         `json:"attempt"`
	Lens           string      `json:"lens,omitempty"`
	Model          string      `json:"model,omitempty"`
	ConfigFile     string      `json:"config_file,omitempty"`
	TimeoutSeconds int         `json:"timeout_seconds,omitempty"`
	ContextRefs    ContextRefs `json:"context_refs"`
}

type RunOptions struct {
	TaskID            string
	AgentType         AgentType
	Attempt           int
	Lens              string
	ContextRefs       ContextRefs
	ConfigFile        string
	PlanFile          string
	TasksDir          string
	WorkDir           string
	OutputRoot        string
	Model             string
	Yolo              bool
	IsolatedCodexHome bool
	Timeout           time.Duration
	StreamOutput      io.Writer
	ProgressOutput    io.Writer
}

type ContextRefs struct {
	SprintID             string       `json:"sprint_id,omitempty"`
	WorktreePath         string       `json:"worktree_path,omitempty"`
	GitHubPRNumber       int          `json:"github_pr_number,omitempty"`
	ArtifactRefs         ArtifactRefs `json:"artifact_refs,omitempty"`
	QAArtifactRefs       ArtifactRefs `json:"latest_qa_artifact_refs,omitempty"`
	ReviewerArtifactRefs ArtifactRefs `json:"latest_reviewer_artifact_refs,omitempty"`
}

type ArtifactRefs struct {
	Log      string `json:"log"`
	Worktree string `json:"worktree"`
	Patch    string `json:"patch"`
	Report   string `json:"report"`
}

type Finding struct {
	ReviewerID         string   `json:"reviewer_id,omitempty"`
	Lens               string   `json:"lens,omitempty"`
	Severity           string   `json:"severity,omitempty"`
	Confidence         string   `json:"confidence,omitempty"`
	Category           string   `json:"category,omitempty"`
	FileRefs           []string `json:"file_refs,omitempty"`
	Summary            string   `json:"summary,omitempty"`
	Evidence           string   `json:"evidence,omitempty"`
	FindingFingerprint string   `json:"finding_fingerprint,omitempty"`
	SuggestedAction    string   `json:"suggested_action,omitempty"`
}

type Result struct {
	Runner             RunnerID     `json:"runner"`
	Status             string       `json:"status"`
	Summary            string       `json:"summary"`
	NextAction         string       `json:"next_action"`
	FailureFingerprint string       `json:"failure_fingerprint"`
	SessionID          string       `json:"session_id,omitempty"`
	ArtifactRefs       ArtifactRefs `json:"artifact_refs"`
	Findings           []Finding    `json:"findings"`
}

type Response struct {
	OK    bool       `json:"ok"`
	Data  *Result    `json:"data,omitempty"`
	Error *ToolError `json:"error,omitempty"`
}

type resultPayload struct {
	Status                  string       `json:"status"`
	Summary                 string       `json:"summary"`
	NextAction              string       `json:"next_action"`
	FailureFingerprint      string       `json:"failure_fingerprint,omitempty"`
	ArtifactRefs            ArtifactRefs `json:"artifact_refs,omitempty"`
	Findings                []Finding    `json:"findings,omitempty"`
	HasFailureFingerprint   bool         `json:"-"`
	FailureFingerprintValid bool         `json:"-"`
	HasArtifactRefs         bool         `json:"-"`
	ArtifactRefsValid       bool         `json:"-"`
	HasFindings             bool         `json:"-"`
	FindingsValid           bool         `json:"-"`
}

func (c ContextRefs) MarshalJSON() ([]byte, error) {
	value := map[string]any{}
	if c.SprintID != "" {
		value["sprint_id"] = c.SprintID
	}
	if c.WorktreePath != "" {
		value["worktree_path"] = c.WorktreePath
	}
	if c.GitHubPRNumber > 0 {
		value["github_pr_number"] = c.GitHubPRNumber
	}
	if hasArtifactRefValues(c.ArtifactRefs) {
		value["artifact_refs"] = c.ArtifactRefs
	}
	if hasArtifactRefValues(c.QAArtifactRefs) {
		value["latest_qa_artifact_refs"] = c.QAArtifactRefs
	}
	if hasArtifactRefValues(c.ReviewerArtifactRefs) {
		value["latest_reviewer_artifact_refs"] = c.ReviewerArtifactRefs
	}
	return json.Marshal(value)
}

func (r ArtifactRefs) MarshalJSON() ([]byte, error) {
	type artifactRefsJSON struct {
		Log      *string `json:"log"`
		Worktree *string `json:"worktree"`
		Patch    *string `json:"patch"`
		Report   *string `json:"report"`
	}

	return json.Marshal(artifactRefsJSON{
		Log:      nullableString(r.Log),
		Worktree: nullableString(r.Worktree),
		Patch:    nullableString(r.Patch),
		Report:   nullableString(r.Report),
	})
}

func (f Finding) MarshalJSON() ([]byte, error) {
	type findingJSON struct {
		ReviewerID         *string  `json:"reviewer_id"`
		Lens               *string  `json:"lens"`
		Severity           *string  `json:"severity"`
		Confidence         *string  `json:"confidence"`
		Category           *string  `json:"category"`
		FileRefs           []string `json:"file_refs"`
		Summary            *string  `json:"summary"`
		Evidence           *string  `json:"evidence"`
		FindingFingerprint *string  `json:"finding_fingerprint"`
		SuggestedAction    *string  `json:"suggested_action"`
	}

	return json.Marshal(findingJSON{
		ReviewerID:         nullableString(f.ReviewerID),
		Lens:               nullableString(f.Lens),
		Severity:           nullableString(f.Severity),
		Confidence:         nullableString(f.Confidence),
		Category:           nullableString(f.Category),
		FileRefs:           f.FileRefs,
		Summary:            nullableString(f.Summary),
		Evidence:           nullableString(f.Evidence),
		FindingFingerprint: nullableString(f.FindingFingerprint),
		SuggestedAction:    nullableString(f.SuggestedAction),
	})
}

func (r Result) MarshalJSON() ([]byte, error) {
	type resultJSON struct {
		Runner             RunnerID     `json:"runner"`
		Status             string       `json:"status"`
		Summary            string       `json:"summary"`
		NextAction         string       `json:"next_action"`
		FailureFingerprint *string      `json:"failure_fingerprint"`
		SessionID          *string      `json:"session_id,omitempty"`
		ArtifactRefs       ArtifactRefs `json:"artifact_refs"`
		Findings           []Finding    `json:"findings"`
	}

	return json.Marshal(resultJSON{
		Runner:             r.Runner,
		Status:             r.Status,
		Summary:            r.Summary,
		NextAction:         r.NextAction,
		FailureFingerprint: nullableString(r.FailureFingerprint),
		SessionID:          nullableString(r.SessionID),
		ArtifactRefs:       r.ArtifactRefs,
		Findings:           r.Findings,
	})
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func hasArtifactRefValues(refs ArtifactRefs) bool {
	return refs.Log != "" || refs.Worktree != "" || refs.Patch != "" || refs.Report != ""
}
