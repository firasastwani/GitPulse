package main

import (
	"fmt"
	"os"

	"github.com/firasastwani/gitpulse/internal/git"
)

func main() {
	repoPath := "."
	remote := "origin"
	branch := "main"

	// Step 1: Open repo
	fmt.Println("Opening repo...")
	mgr, err := git.New(repoPath, remote, branch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "New failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Repo opened successfully.")

	// Step 2: Stage all changed files
	files := []string{
		"internal/git/git.go",
		"internal/watcher/watcher.go",
		"internal/grouper/grouper.go",
	}
	fmt.Println("Staging files...")
	if err := mgr.StageFiles(files); err != nil {
		fmt.Fprintf(os.Stderr, "StageFiles failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Files staged successfully.")

	// Step 3: Get staged diff
	fmt.Println("Getting staged diff...")
	diff, err := mgr.GetStagedDiff()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetStagedDiff failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Staged diff:\n%s\n\n", diff)

	// Step 4: Commit
	msg := "implement watcher, grouper, and git manager"
	fmt.Printf("Committing with message: %q\n", msg)
	hash, err := mgr.Commit(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Commit failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Committed: %s\n", hash)

	// Step 5: Push
	fmt.Println("Pushing to remote...")
	if err := mgr.Push(); err != nil {
		fmt.Fprintf(os.Stderr, "Push failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Pushed successfully!")
}
