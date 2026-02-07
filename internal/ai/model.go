package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/firasastwani/gitpulse/internal/grouper"
)

const anthropicAPI = "https://api.anthropic.com/v1/messages"

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

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response body from the Anthropic Messages API.
type anthropicResponse struct {
	Content []contentBlock `json:"content"`
	Error   *apiError      `json:"error,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// callClaude sends a prompt to the Claude API and returns the text response.
func (c *Client) callClaude(prompt string) (string, error) {
	return c.callClaudeWithTokens(prompt, 1024)
}

// callClaudeWithTokens sends a prompt with a custom max_tokens limit.
func (c *Client) callClaudeWithTokens(prompt string, maxTokens int) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	for _, block := range apiResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("no text content in response")
}

// RefineAndCommit sends pre-grouped file changes to Claude for semantic
// refinement and commit message generation in a single API call.
//
// Input: heuristic pre-groups with diffs
// Output: refined groups with AI-generated commit messages
//
// If the API call fails, returns the original groups unchanged (graceful fallback).
func (c *Client) RefineAndCommit(groups []grouper.FileGroup) ([]grouper.FileGroup, error) {
	var sb strings.Builder
	sb.WriteString("You are a git commit assistant. Analyze the following pre-grouped file changes and:\n")
	sb.WriteString("1. Refine the groupings if files should be moved between groups\n")
	sb.WriteString("2. Generate a specific, descriptive conventional commit message for each group.\n")
	sb.WriteString("   - The message MUST describe WHAT changed, not just that something changed.\n")
	sb.WriteString("   - BAD:  'refactor(ui): update logger implementation'\n")
	sb.WriteString("   - GOOD: 'feat(ui): add interactive code review prompts with severity-colored findings display'\n")
	sb.WriteString("   - BAD:  'chore: auto-commit changes'\n")
	sb.WriteString("   - GOOD: 'feat(config): add CodeReview toggle to AIConfig for optional pre-push review'\n")
	sb.WriteString("   - Include the specific behavior or feature, not generic verbs like 'update' or 'modify'\n\n")
	sb.WriteString("Respond with ONLY valid JSON in this exact format:\n")
	sb.WriteString(`[{"files":["path/to/file.go"],"reason":"why grouped","commit_message":"feat: description"}]`)
	sb.WriteString("\n\nPre-grouped changes:\n\n")

	for i, g := range groups {
		sb.WriteString(fmt.Sprintf("Group %d (%s):\n", i+1, g.Reason))
		sb.WriteString(fmt.Sprintf("  Files: %s\n", strings.Join(g.Files, ", ")))
		if g.Diffs != "" {
			sb.WriteString(fmt.Sprintf("  Diff:\n%s\n", g.Diffs))
		}
		sb.WriteString("\n")
	}

	text, err := c.callClaude(sb.String())
	if err != nil {
		return groups, fmt.Errorf("claude API call failed: %w", err)
	}

	text = stripCodeFences(text)

	var refined []struct {
		Files         []string `json:"files"`
		Reason        string   `json:"reason"`
		CommitMessage string   `json:"commit_message"`
	}

	if err := json.Unmarshal([]byte(text), &refined); err != nil {
		// fallback: keep original groups, generate commit messages individually
		for i := range groups {
			msg, msgErr := c.GenerateCommitMessage(groups[i].Diffs, groups[i].Files)
			if msgErr == nil {
				groups[i].CommitMessage = msg
			}
		}
		return groups, nil
	}

	// Build file -> diff lookup from original groups so diffs survive refinement
	// Parse the original diffs to extract per-file diff sections
	fileDiffs := make(map[string]string)
	for _, g := range groups {
		if len(g.Files) == 1 {
			// Single file group: entire diff belongs to that file
			fileDiffs[g.Files[0]] = g.Diffs
		} else {
			// Multiple files: split diff by file headers
			diffSections := strings.Split(g.Diffs, "diff --git")
			for _, section := range diffSections {
				if section == "" {
					continue
				}
				section = "diff --git" + section
				// Extract filename from diff header
				for _, f := range g.Files {
					if strings.Contains(section, " a/"+f+" ") || strings.Contains(section, " b/"+f+" ") {
						fileDiffs[f] = section
						break
					}
				}
			}
		}
	}

	refinedGroups := make([]grouper.FileGroup, len(refined))
	for i, r := range refined {
		// Reconstruct diffs for the refined group from original per-file diffs
		var combinedDiffs strings.Builder
		for _, f := range r.Files {
			if d, ok := fileDiffs[f]; ok {
				combinedDiffs.WriteString(d)
			}
		}
		var combinedDiffsStr string
		if combinedDiffs.Len() > 0 {
			combinedDiffsStr = combinedDiffs.String()
		}

		refinedGroups[i] = grouper.FileGroup{
			Files:         r.Files,
			Reason:        r.Reason,
			CommitMessage: r.CommitMessage,
			Diffs:         combinedDiffs,
		}
	}

	return refinedGroups, nil
}

// stripCodeFences removes markdown code fences that Claude sometimes wraps around JSON.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = s[len("```json"):]
	} else if strings.HasPrefix(s, "```") {
		s = s[len("```"):]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// GenerateCommitMessage generates a commit message for a single group's diff.
// Used as fallback when RefineAndCommit fails for individual groups.
func (c *Client) GenerateCommitMessage(diff string, files []string) (string, error) {
	prompt := fmt.Sprintf(
		"Generate a single git commit message using conventional commits format "+
			"(feat/fix/refactor/chore/docs/test).\n\n"+
			"The message MUST be specific about WHAT changed — describe the actual behavior or feature added.\n"+
			"BAD:  'refactor(engine): update engine implementation'\n"+
			"GOOD: 'feat(engine): add AI code review gate with interactive fix/continue prompt before push'\n"+
			"Avoid generic verbs like 'update', 'modify', 'change' — say what was actually done.\n\n"+
			"Files changed: %s\n\nDiff:\n%s\n\n"+
			"Respond with ONLY the commit message, nothing else.",
		strings.Join(files, ", "), diff,
	)

	msg, err := c.callClaude(prompt)
	if err != nil {
		return "chore: auto-commit changes", fmt.Errorf("claude API call failed: %w", err)
	}

	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "chore: auto-commit changes", nil
	}

	return msg, nil
}


