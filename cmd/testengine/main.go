package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/engine"
	"github.com/firasastwani/gitpulse/internal/ui"
	"github.com/firasastwani/gitpulse/internal/watcher"
)

func main() {
	// ── Load config (reads config.yaml + .env) ──
	logger := ui.New(nil)
	logger.Info("=== Engine Pipeline Test ===")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.Error("Failed to load config", err)
		os.Exit(1)
	}

	if cfg.AI.APIKey == "" {
		fmt.Fprintln(os.Stderr, "No API key found. Set CLAUDE_API_KEY in .env")
		os.Exit(1)
	}
	logger.Info("Config loaded", "model", cfg.AI.Model, "auto_push", cfg.AutoPush)

	// ── Detect real changed files ──
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		logger.Error("git status failed", err)
		os.Exit(1)
	}

	var changes []watcher.FileChange
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := line[3:]

		// skip test binaries
		if path == "testpipeline" || path == "testgit" || path == "testengine" {
			continue
		}

		var changeType watcher.ChangeType
		trimmed := strings.TrimSpace(status)
		switch {
		case trimmed == "??" || trimmed == "A":
			changeType = watcher.Created
		case trimmed == "D":
			changeType = watcher.Deleted
		case trimmed == "R":
			changeType = watcher.Renamed
		default:
			changeType = watcher.Modified
		}

		changes = append(changes, watcher.FileChange{Path: path, Type: changeType})
	}

	if len(changes) == 0 {
		logger.Info("No changes detected. Nothing to do.")
		return
	}

	logger.Info("Detected changes", "count", len(changes))
	for _, fc := range changes {
		logger.Info("  file", "path", fc.Path, "type", fc.Type)
	}

	// ── Create engine and run processChanges directly ──
	eng, err := engine.New(cfg, logger)
	if err != nil {
		logger.Error("Failed to create engine", err)
		os.Exit(1)
	}

	// Start engine so it can buffer, then manually flush
	go eng.Run()

	// Simulate the watcher having buffered these changes
	// by sending them to the engine's watcher events channel
	// Instead, directly test Flush after the watcher buffers
	logger.Info("Flushing engine pipeline...")

	// We can't inject into the watcher, so just use Flush with pending set externally
	// For a clean test, just build and use `gitpulse` + `gitpulse push` flow
	_ = changes
	eng.Flush()

	logger.Info("=== Engine Pipeline Test Complete ===")
}
