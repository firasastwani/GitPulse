package git

// Manager handles all git operations for a repository.
type Manager struct {
	repoPath string
	remote   string
	branch   string
}

// New creates a new git Manager for the given repository path.
func New(repoPath, remote, branch string) (*Manager, error) {
	// TODO: Open repository with go-git, validate it exists
	return &Manager{
		repoPath: repoPath,
		remote:   remote,
		branch:   branch,
	}, nil
}

// StageFiles adds the specified files to the git staging area.
func (m *Manager) StageFiles(files []string) error {
	// TODO: Use go-git worktree.Add() for each file
	return nil
}

// GetStagedDiff returns the unified diff of all currently staged changes.
func (m *Manager) GetStagedDiff() (string, error) {
	// TODO: Get diff between HEAD and staging area
	return "", nil
}

// GetFileDiff returns the diff for a specific file (staged or unstaged).
func (m *Manager) GetFileDiff(path string) (string, error) {
	// TODO: Get diff for individual file
	return "", nil
}

// Commit creates a new commit with the given message.
// Returns the commit hash.
func (m *Manager) Commit(message string) (string, error) {
	// TODO: Create commit with go-git
	return "", nil
}

// Push pushes commits to the configured remote/branch.
func (m *Manager) Push() error {
	// TODO: Push with go-git, fallback to exec("git push") if auth issues
	return nil
}

// ResetStaging unstages all currently staged files.
func (m *Manager) ResetStaging() error {
	// TODO: Reset staging area
	return nil
}
