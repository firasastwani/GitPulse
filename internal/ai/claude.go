package ai

import (
	"github.com/firasastwani/gitpulse/internal/grouper"
)

// Client handles communication with the Claude API.
type Client struct {
	apiKey string
	model  string
}

// NewClient creates a new Claude API client.
func NewClient(apiKey, model string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  model,
	}
}

// RefineAndCommit sends pre-grouped file changes to Claude for semantic
// refinement and commit message generation in a single API call.
//
// Input: heuristic pre-groups with diffs
// Output: refined groups with AI-generated commit messages
//
// If the API call fails, returns the original groups unchanged (graceful fallback).
func (c *Client) RefineAndCommit(groups []grouper.FileGroup) ([]grouper.FileGroup, error) {
	// TODO: Build prompt with pre-groups + diffs
	// TODO: Send HTTP POST to Claude API
	// TODO: Parse JSON response into refined FileGroups
	// TODO: Fallback to original groups on error
	return groups, nil
}

// GenerateCommitMessage generates a commit message for a single group's diff.
// Used as fallback when RefineAndCommit fails for individual groups.
func (c *Client) GenerateCommitMessage(diff string, files []string) (string, error) {
	// TODO: Build commit message prompt
	// TODO: Send to Claude API
	// TODO: Parse response
	return "chore: auto-commit changes", nil
}
