package agentrun

import (
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

type RunOptions struct {
	TaskID         string
	AgentType      AgentType
	Attempt        int
	Lens           string
	ContextRefs    ContextRefs
	ConfigFile     string
	PlanFile       string
	TasksDir       string
	WorkDir        string
	OutputRoot     string
	Model          string
	Timeout        time.Duration
	StreamOutput   io.Writer
	ProgressOutput io.Writer
}

type ContextRefs struct {
	SprintID       string
	WorktreePath   string
	GitHubPRNumber int
	ArtifactRefs   ArtifactRefs
}

type ArtifactRefs struct {
	Log      string `json:"log,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Patch    string `json:"patch,omitempty"`
	Report   string `json:"report,omitempty"`
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
	FailureFingerprint string       `json:"failure_fingerprint,omitempty"`
	SessionID          string       `json:"session_id,omitempty"`
	ArtifactRefs       ArtifactRefs `json:"artifact_refs,omitempty"`
	Findings           []Finding    `json:"findings,omitempty"`
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
