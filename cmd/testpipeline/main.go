package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/firasastwani/gitpulse/internal/ai"
	"github.com/firasastwani/gitpulse/internal/git"
	"github.com/firasastwani/gitpulse/internal/grouper"
	"github.com/firasastwani/gitpulse/internal/watcher"
)

func main() {
	repoPath := "."
	remote := "origin"
	branch := "main"

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "Set ANTHROPIC_API_KEY env var before running.")
		os.Exit(1)
	}
	model := "claude-sonnet-4-5"

	// ── Step 1: Open repo ──
	fmt.Println("=== Step 1: Open repo ===")
	mgr, err := git.New(repoPath, remote, branch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open repo: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Repo opened.")

	// ── Step 2: Detect real changed files via git status ──
	fmt.Println("\n=== Step 2: Detect changed files ===")
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git status failed: %v\n", err)
		os.Exit(1)
	}

	var changes []watcher.FileChange
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// porcelain format: XY<space>PATH (always 2 char status + space + path)
		status := line[:2]
		path := line[3:]

		// skip binaries
		if path == "testgit" || path == "testpipeline" {
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
		fmt.Printf("  [%s] %s\n", status, path)
	}

	if len(changes) == 0 {
		fmt.Println("  No changes detected. Nothing to do.")
		return
	}

	changeset := watcher.ChangeSet{
		Files:     changes,
		Timestamp: time.Now(),
	}

	// ── Step 3: Heuristic pre-grouping ──
	fmt.Println("\n=== Step 3: PreGroup (heuristic) ===")
	groups := grouper.PreGroup(changeset)
	for i, g := range groups {
		fmt.Printf("  Group %d: %v\n", i+1, g.Files)
		fmt.Printf("    Reason: %s\n", g.Reason)
	}

	// ── Step 4: Get real diffs for each group ──
	fmt.Println("\n=== Step 4: Get diffs ===")
	for i := range groups {
		for _, f := range groups[i].Files {
			d, err := mgr.GetFileDiff(f)
			if err != nil {
				d = fmt.Sprintf("--- /dev/null\n+++ b/%s\n(new or deleted file)", f)
			}
			groups[i].Diffs += d + "\n"
		}
		fmt.Printf("  Group %d: %d bytes of diff\n", i+1, len(groups[i].Diffs))
	}

	// ── Step 5: Claude refinement + commit messages ──
	fmt.Println("\n=== Step 5: Claude RefineAndCommit ===")
	aiClient := ai.NewClient(apiKey, model)

	refined, err := aiClient.RefineAndCommit(groups)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  RefineAndCommit failed: %v\n", err)
		fmt.Println("  Falling back to original groups with default messages.")
		refined = groups
		for i := range refined {
			if refined[i].CommitMessage == "" {
				refined[i].CommitMessage = "chore: auto-commit changes"
			}
		}
	}

	for i, g := range refined {
		fmt.Printf("  Group %d:\n", i+1)
		fmt.Printf("    Files:  %v\n", g.Files)
		fmt.Printf("    Reason: %s\n", g.Reason)
		fmt.Printf("    Commit: %s\n", g.CommitMessage)
	}

	// ── Step 6: Stage + Commit each group ──
	fmt.Println("\n=== Step 6: Stage & Commit per group ===")

	// Reset staging area first to start clean
	if err := mgr.ResetStaging(); err != nil {
		fmt.Fprintf(os.Stderr, "  ResetStaging failed: %v\n", err)
	}

	for i, g := range refined {
		fmt.Printf("\n  --- Group %d ---\n", i+1)

		// Stage files for this group
		fmt.Printf("  Staging: %v\n", g.Files)
		if err := mgr.StageFiles(g.Files); err != nil {
			fmt.Fprintf(os.Stderr, "  StageFiles failed: %v\n", err)
			continue
		}

		// Commit with Claude's message
		hash, err := mgr.Commit(g.CommitMessage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Commit failed: %v\n", err)
			continue
		}
		fmt.Printf("  Committed: %s  ->  %s\n", hash[:8], g.CommitMessage)
	}

	// ── Step 7: Push all commits ──
	fmt.Println("\n=== Step 7: Push ===")
	if err := mgr.Push(); err != nil {
		fmt.Fprintf(os.Stderr, "  Push failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Pushed successfully!")

	fmt.Println("\n=== Full pipeline complete! ===")
}
