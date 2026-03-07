package git

type BranchLocation string

const (
	BranchLocationLocal          BranchLocation = "local"
	BranchLocationRemoteTracking BranchLocation = "remote_tracking"

	defaultRemoteName = "origin"
)

type FetchRequest struct {
	WorkDir  string
	Remote   string
	Refspecs []string
	Prune    bool
}

type BranchExistsRequest struct {
	WorkDir  string
	Branch   string
	Location BranchLocation
	Remote   string
}

type CreateBranchRequest struct {
	WorkDir    string
	Branch     string
	StartPoint string
	Force      bool
}

type CheckoutRequest struct {
	WorkDir    string
	Branch     string
	StartPoint string
	Create     bool
	Force      bool
}

type AddWorktreeRequest struct {
	WorkDir      string
	Path         string
	Branch       string
	StartPoint   string
	CreateBranch bool
	Force        bool
}

type ListWorktreesRequest struct {
	WorkDir string
}

type RemoveWorktreeRequest struct {
	WorkDir string
	Path    string
	Force   bool
}

type RevParseRequest struct {
	WorkDir   string
	Revision  string
	AbbrevRef bool
}

type MergeBaseRequest struct {
	WorkDir       string
	LeftRevision  string
	RightRevision string
}

type PushRequest struct {
	WorkDir     string
	Remote      string
	Refspecs    []string
	SetUpstream bool
	Force       bool
}

type Worktree struct {
	Path           string
	HeadSHA        string
	Branch         string
	BranchRef      string
	Detached       bool
	Bare           bool
	Locked         bool
	LockReason     string
	Prunable       bool
	PrunableReason string
}
