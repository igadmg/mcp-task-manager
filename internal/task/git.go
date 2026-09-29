package task

// GitRepo is the code repository the branch-per-task workflow drives. It is
// implemented by *vcs.Repo and exported so external test packages can wrap
// it for fault injection. A Service without one has branching off.
type GitRepo interface {
	// Check verifies the repository can take branch operations.
	Check() error
	// Head returns the checked-out branch and its commit.
	Head() (branch, sha string, err error)
	ResolveCommit(rev string) (string, error)
	IsAncestor(a, b string) (bool, error)
	TreeOf(rev string) (string, error)

	BranchSHA(name string) (sha string, ok bool, err error)
	// FirstExistingBranch returns an empty name when none of names exists.
	FirstExistingBranch(names []string) (name, sha string, err error)
	BranchAvailable(name string) error
	CreateBranch(name, sha string) error
	MoveBranch(name, newSHA, oldSHA string) error
	DeleteBranch(name, expectSHA string) error
	Switch(name string) error
	ResetKeep(sha string) error

	// DirtyPaths lists changed paths outside the tasks directory.
	DirtyPaths() ([]string, error)
	IndexTree() (string, error)
	RestoreIndex(tree string) error
	// Snapshot commits every change outside the tasks directory onto the
	// checked-out branch without touching the worktree.
	Snapshot(msg string) (sha string, committed bool, err error)
	MergeTrees(base, ours, theirs string) (tree string, conflicts []string, err error)
	CommitTree(tree, parent, msg string) (string, error)
	Replay(onto, upstream, branch string) (newTip string, conflicts []string, err error)
}

// Identity is the user branches are namespaced under and whose
// current-task pointer the service keeps.
type Identity struct {
	Name string
	// FromGitEmail is false when the name fell back to the OS user.
	FromGitEmail bool
}

// CurrentTaskStore keeps each user's current-task pointer.
type CurrentTaskStore interface {
	ReadCurrentTask(user string) (id string, ok bool, err error)
	WriteCurrentTask(user, id string) error
	RemoveCurrentTask(user string) error
}

// ServiceOption configures optional Service collaborators.
type ServiceOption func(*Service)

// WithGit turns the branch-per-task workflow on, driving g. Nil leaves
// branching off.
func WithGit(g GitRepo) ServiceOption {
	return func(s *Service) { s.git = g }
}

// WithIdentity sets the user branches and the current-task pointer belong to.
func WithIdentity(id Identity) ServiceOption {
	return func(s *Service) { s.identity = id }
}

// WithCurrentTaskStore sets where current-task pointers are kept. Without
// one, no pointer is kept.
func WithCurrentTaskStore(c CurrentTaskStore) ServiceOption {
	return func(s *Service) { s.current = c }
}
