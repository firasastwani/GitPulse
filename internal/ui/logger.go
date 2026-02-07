package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/firasastwani/gitpulse/internal/ai"
)

// ANSI color codes for terminal output.
const (
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

// Logger wraps charmbracelet/log for styled GitPulse output.
type Logger struct {
	logger *log.Logger
	reader *bufio.Reader
}

// New creates a new styled Logger.
func New() *Logger {
	l := log.NewWithOptions(os.Stdout, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "15:04:05",
	})

	return &Logger{
		logger: l,
		reader: bufio.NewReader(os.Stdin),
	}
}

// Info logs an informational message with optional key-value pairs.
func (l *Logger) Info(msg string, keyvals ...interface{}) {
	l.logger.Info(msg, keyvals...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, keyvals ...interface{}) {
	l.logger.Warn(msg, keyvals...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, err error, keyvals ...interface{}) {
	kv := append([]interface{}{"err", err}, keyvals...)
	l.logger.Error(msg, kv...)
}

// GroupInfo logs semantic grouping results in a tree-like format.
func (l *Logger) GroupInfo(groupCount int, groups []GroupDisplay) {
	l.logger.Info(fmt.Sprintf("Semantic grouping: %d groups", groupCount))
	for i, g := range groups {
		prefix := "├─"
		if i == len(groups)-1 {
			prefix = "└─"
		}
		fmt.Printf("  %s Group %d: %s\n", prefix, i+1, g.Files)
		fmt.Printf("     reason: %q\n", g.Reason)
	}
}

// GroupDisplay holds display info for a file group.
type GroupDisplay struct {
	Files  string
	Reason string
}

// CommitSuccess logs a successful commit.
func (l *Logger) CommitSuccess(hash, message string) {
	l.logger.Info("Committed", "hash", hash[:7], "msg", message)
}

// PushSuccess logs a successful push.
func (l *Logger) PushSuccess(commitCount int, remote string) {
	l.logger.Info(fmt.Sprintf("Pushed %d commits", commitCount), "remote", remote)
}

// ReviewFindings renders code review findings in a styled, tree-like format.
// Errors are red, warnings are yellow, info is cyan.
func (l *Logger) ReviewFindings(findings []ai.ReviewFinding) {
	blockerCount := 0
	for _, f := range findings {
		if f.Severity == ai.SeverityError || f.Severity == ai.SeverityWarning {
			blockerCount++
		}
	}

	l.logger.Warn(fmt.Sprintf("Code review found %d issue(s), %d blocking", len(findings), blockerCount))
	fmt.Println()

	for i, f := range findings {
		// Tree connector
		prefix := "├─"
		if i == len(findings)-1 {
			prefix = "└─"
		}

		// Color based on severity
		var color string
		var label string
		switch f.Severity {
		case ai.SeverityError:
			color = colorRed
			label = "ERROR"
		case ai.SeverityWarning:
			color = colorYellow
			label = "WARNING"
		default:
			color = colorCyan
			label = "INFO"
		}

		// Primary location
		lineRange := fmt.Sprintf("L%d", f.StartLine)
		if f.EndLine > f.StartLine {
			lineRange = fmt.Sprintf("L%d-%d", f.StartLine, f.EndLine)
		}

		fmt.Printf("  %s %s[%s]%s %s %s(%s)%s\n",
			prefix, color, label, colorReset,
			f.File, colorGray, lineRange, colorReset)
		fmt.Printf("     %s%s%s\n", colorBold, f.Description, colorReset)

		if f.Suggestion != "" {
			fmt.Printf("     %sfix: %s%s\n", colorGray, f.Suggestion, colorReset)
		}

		// Related locations
		for j, loc := range f.RelatedLocations {
			relPrefix := "│  ├─"
			if j == len(f.RelatedLocations)-1 {
				relPrefix = "│  └─"
			}
			fmt.Printf("     %s %salso see: %s (L%d-%d)%s\n",
				relPrefix, colorGray, loc.File, loc.StartLine, loc.EndLine, colorReset)
		}
	}
	fmt.Println()
}

// PromptReviewAction displays the 3 review options and reads the user's choice.
// Returns "manual", "aifix", or "continue".
func (l *Logger) PromptReviewAction() (string, error) {
	fmt.Println(colorBold + "  How would you like to proceed?" + colorReset)
	fmt.Println("    [1] Fix manually (pause and re-review after)")
	fmt.Println("    [2] Let AI fix")
	fmt.Println("    [3] Continue anyway (push with current code)")
	fmt.Print("\n  Choice [1/2/3]: ")

	input, err := l.reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	switch strings.TrimSpace(input) {
	case "1":
		return "manual", nil
	case "2":
		return "aifix", nil
	case "3":
		return "continue", nil
	default:
		// Invalid input — default to continue to avoid blocking
		l.logger.Warn("Invalid choice, defaulting to continue")
		return "continue", nil
	}
}

// WaitForManualFix prints instructions and blocks until the user presses ENTER.
func (l *Logger) WaitForManualFix() error {
	fmt.Println()
	l.logger.Info("Fix the issues in your editor, then press ENTER to re-review...")
	_, err := l.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	return nil
}

// AIFixApplied logs that an AI-generated fix was written to a file.
func (l *Logger) AIFixApplied(file, description string) {
	l.logger.Info("AI fix applied", "file", file, "fix", description)
}
