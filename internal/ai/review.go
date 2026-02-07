package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/firasastwani/gitpulse/internal/grouper"
)

// define severity levels for review findings
const (
	SeverityError   = "error"   // a bug/logic error is found. Blocks push
	SeverityWarning = "warning" // code has a POTENTIAL issue. Blocks push
	SeverityInfo    = "info"    // suggestions/ Does not block push
)

// Location represents a specific code location (a line range in a file).
type Location struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// ReviewFinding represents a single issue found during AI code review.
type ReviewFinding struct {
	File             string     `json:"file"`
	StartLine        int        `json:"start_line"`
	EndLine          int        `json:"end_line"`
	Severity         string     `json:"severity"` // "error", "warning", or "info"
	Description      string     `json:"description"`
	Suggestion       string     `json:"suggestion"`
	RelatedLocations []Location `json:"related_locations,omitempty"` // for multi file errors
}

type ReviewResult struct {
	Findings    []ReviewFinding
	HasBlockers bool // if severity is 'error' or 'warning' blocks the push
}

// ReviewCode sends the file diffs to Claude for code review
// Analyzes each group's diffs looking for either bugs, logic errors, secuirty issues
// or some other problems that should be addressed before a push.
// Returns a ReviewResult with the findings. If no issues are found, Findings
// will be empty and HasBlockers will be false, allowing the push to continue
// If the API call fails, it returns an error and the push continues
func (c *Client) ReviewCode(groups []grouper.FileGroup) (*ReviewResult, error) {

	var sb strings.Builder

	// prompting
	sb.WriteString("You are an expert code reviewer. Analyze the following file diffs and identify:\n")
	sb.WriteString("1. Bugs and logic errors\n")
	sb.WriteString("2. Security vulnerabilities\n")
	sb.WriteString("3. Nil pointer / index out of bounds risks\n")
	sb.WriteString("4. Race conditions or concurrency issues\n")
	sb.WriteString("5. Obvious mistakes (typos in logic, wrong variable, missing error handling)\n\n")
	sb.WriteString("Do NOT flag style issues, naming preferences, or minor nits.\n")
	sb.WriteString("Only report genuine problems that could cause bugs or security issues.\n\n")
	sb.WriteString("If you find NO issues, respond with an empty JSON array: []\n\n")
	sb.WriteString("For issues spanning multiple lines, use start_line and end_line to indicate the range.\n")
	sb.WriteString("For issues involving multiple files, include related_locations to reference the connected code.\n\n")
	sb.WriteString("Respond with ONLY valid JSON in this exact format:\n")
	sb.WriteString(`[{"file":"path/to/file.go","start_line":42,"end_line":50,"severity":"error|warning|info","description":"what is wrong","suggestion":"how to fix it","related_locations":[{"file":"path/to/other.go","start_line":10,"end_line":12}]}]`)
	sb.WriteString("\n\nFile diffs to review:\n\n")

	for i, g := range groups {
		sb.WriteString(fmt.Sprintf("=== Group %d ===\n", i+1))
		sb.WriteString(fmt.Sprintf("Files: %s\n", strings.Join(g.Files, ", ")))
		if g.Diffs != "" {
			sb.WriteString(fmt.Sprintf("Diff:\n%s\n", g.Diffs))
		}
		sb.WriteString("\n")
	}

	text, err := c.callClaude(sb.String())

	if err != nil {
		return nil, fmt.Errorf("code review API call failed: %w", err)
	}

	// gets rid of claude fluff
	text = stripCodeFences(text)

	var findings []ReviewFinding

	if err := json.Unmarshal([]byte(text), &findings); err != nil {
		return nil, fmt.Errorf("failed to parse review response: %w (raw: %s)", err, truncate(text, 200))
	}

	// Validate and normalize severity values
	for i := range findings {
		switch findings[i].Severity {
		case SeverityError, SeverityWarning, SeverityInfo:
			// valid — keep as is
		default:
			// unknown severity — default to warning to be safe
			findings[i].Severity = SeverityWarning
		}

		// If end_line not set, default to start_line (single-line finding)
		if findings[i].EndLine == 0 && findings[i].StartLine > 0 {
			findings[i].EndLine = findings[i].StartLine
		}
	}

	result := &ReviewResult{
		Findings:    findings,
		HasBlockers: hasBlockers(findings),
	}

	return result, nil
}

// GenerateFix sends a file's current content, a review finding, and related
// file contents to Claude, asking it to produce corrected file content.
//
// primaryContent is the content of the file where the main issue lives.
// relatedContents maps file paths to their content for cross-file context.
//
// Returns the full corrected primary file content as a string, ready to be
// written back to disk.
func (c *Client) GenerateFix(filePath string, finding ReviewFinding, primaryContent string, relatedContents map[string]string) (string, error) {
	var sb strings.Builder
	sb.WriteString("You are a code fixer. A code review found the following issue:\n\n")
	sb.WriteString(fmt.Sprintf("File: %s\n", filePath))
	sb.WriteString(fmt.Sprintf("Lines: %d-%d\n", finding.StartLine, finding.EndLine))
	sb.WriteString(fmt.Sprintf("Severity: %s\n", finding.Severity))
	sb.WriteString(fmt.Sprintf("Problem: %s\n", finding.Description))
	sb.WriteString(fmt.Sprintf("Suggestion: %s\n\n", finding.Suggestion))

	sb.WriteString(fmt.Sprintf("Here is the primary file content (%s):\n\n```\n%s\n```\n\n", filePath, primaryContent))

	// Include related file contents for cross-file context
	if len(finding.RelatedLocations) > 0 && len(relatedContents) > 0 {
		sb.WriteString("Related files for context:\n\n")
		for _, loc := range finding.RelatedLocations {
			if content, ok := relatedContents[loc.File]; ok {
				sb.WriteString(fmt.Sprintf("--- %s (lines %d-%d relevant) ---\n```\n%s\n```\n\n",
					loc.File, loc.StartLine, loc.EndLine, content))
			}
		}
	}

	sb.WriteString("Return the COMPLETE corrected content for the primary file with the issue fixed.\n")
	sb.WriteString("Respond with ONLY the file content, no explanations, no markdown fences.\n")
	sb.WriteString("Do not change anything else besides fixing the identified issue.")

	fixed, err := c.callClaude(sb.String())
	if err != nil {
		return "", fmt.Errorf("fix generation failed for %s: %w", filePath, err)
	}

	// Claude sometimes wraps the response in code fences despite instructions
	fixed = stripCodeFences(fixed)

	if strings.TrimSpace(fixed) == "" {
		return "", fmt.Errorf("AI returned empty fix for %s", filePath)
	}

	return fixed, nil
}

// hasBlockers returns true if any finding has severity "error" or "warning".
func hasBlockers(findings []ReviewFinding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError || f.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
