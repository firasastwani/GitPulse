package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
)

// Logger wraps charmbracelet/log for styled GitPulse output.
type Logger struct {
	logger *log.Logger
}

// New creates a new styled Logger.
func New() *Logger {
	l := log.NewWithOptions(os.Stdout, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "15:04:05",
	})

	return &Logger{logger: l}
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
