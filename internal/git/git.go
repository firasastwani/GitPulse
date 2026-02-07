package git

import (
	"fmt"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/config"
)

// Manager handles all git operations for a repository.
type Manager struct {
	repoPath string
	remote   string
	branch   string
	repo     *gogit.Repository
}

// New creates a new git Manager for the given repository path.
func New(repoPath, remote, branch string) (*Manager, error) {

	repo, err := gogit.PlainOpen(repoPath)

	// catch error, very likely needs to be fixed
	if err != nil {
		return nil, fmt.Errorf("failed to open repo at %s: %w", repoPath, err)
	}

	return &Manager{
		repoPath: repoPath,
		remote:   remote,
		branch:   branch,
		repo:     repo,
	}, nil
}

// StageFiles adds the specified files to the git staging area.

// common error location.. can add more logging etc
func (m *Manager) StageFiles(files []string) error {

	wt, err := m.repo.Worktree()

	if err != nil {
		return fmt.Errorf("Failed to get worktree %w", err)
	}

	for _, f := range files {
		_, err := wt.Add(f)

		if err != nil {
			return fmt.Errorf("failed to stage %s: %w", f, err)
		}
	}

	return nil
}

// GetStagedDiff returns the unified diff of all currently staged changes.
func (m *Manager) GetStagedDiff() (string, error) {
	// TODO: Get diff between HEAD and staging area

	wt, err := m.repo.Worktree()

	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := wt.Status()

	if err != nil {
		return "", fmt.Errorf("failed to get status: %w", err)
	}

	// collect all the paths that are staged

	var staged []string

	for path, s := range status {
		if s.Staging != gogit.Unmodified && s.Staging != gogit.Untracked {
			staged = append(staged, path)
		}
	}

	var diffs []string

	for _, path := range staged {
		d, err := m.GetFileDiff(path)

		if err != nil {
			// dosent return error so it dosent break all if one stops
			continue
		}

		diffs = append(diffs, d)
	}

	return strings.Join(diffs, "\n"), nil
}

// GetFileDiff returns the diff for a specific file (staged or unstaged).
func (m *Manager) GetFileDiff(path string) (string, error) {

	head, err := m.repo.Head()

	if err != nil {
		return "", fmt.Errorf("failed to get head: %w", err)
	}

	commitObj, err := m.repo.CommitObject(head.Hash())

	if err != nil {
		return "", fmt.Errorf("failed to get head commit: %w", err)
	}

	headTree, err := commitObj.Tree()
	
	if err != nil {
		return "", fmt.Errorf("failed to get head tree: %w", err)
	}

	// get file from head tree

	headFile, err := headTree.File(path)
	if err != nil {
		// file is new, not in head. returns empty old content (entire file is the diff)
		return fmt.Sprintf("--- /dev/null\n+++ b/%s\n(new file)", path), nil
	}

	oldContent, err := headFile.Contents()
	if err != nil {
		return "", fmt.Errorf("failed to read HEAD version of %s: %w", path, err)
	}

	return fmt.Sprintf("--- a/%s\n+++ b/%s\n(diff content for %s, old size: %d bytes)",
	path, path, path, len(oldContent)), nil
}

// Commit creates a new commit with the given message.
// Returns the commit hash.
func (m *Manager) Commit(message string) (string, error) {

	wt, err := m.repo.Worktree()

	if err != nil {
		return "", fmt.Errorf("failed to get worktree: %w", err)
	}

	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name: "GitPulse",
			Email: "gitpulse@auto",
			When: time.Now(),
		},
	})

	if err != nil {
		return "", fmt.Errorf("failed to commit changes: %w", err)
	}	

	return hash.String(), nil
}

// Push pushes commits to the configured remote/branch.
func (m *Manager) Push() error {
	err := m.repo.Push(&gogit.PushOptions{
		RemoteName: m.remote,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/" + m.branch + ":refs/heads/" + m.branch),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to push to %s/%s: %w", m.remote, m.branch, err)
	}

	return nil
}

// ResetStaging unstages all currently staged files.
func (m *Manager) ResetStaging() error {

	wt, err := m.repo.Worktree()

	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = wt.Reset(&gogit.ResetOptions{
		Mode: gogit.MixedReset,
	})

	if err != nil {
		return fmt.Errorf("failed to reset staging: %w", err)
	}

	return nil
}
