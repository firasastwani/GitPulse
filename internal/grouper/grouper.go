package grouper

import (
	"path/filepath"
	"strings"

	"github.com/firasastwani/gitpulse/internal/watcher"
)

// TODO: in the future I could use the contents of the file itself rather than just names and directories
// for simplicity rn, ->

// FileGroup represents a semantically related set of file changes.
type FileGroup struct {
	Files         []string // paths relative to repo root
	Reason        string   // why these files are grouped (e.g., "same package: internal/auth")
	Diffs         string   // combined unified diff for all files in group
	CommitMessage string   // AI-generated commit message (populated after AI refinement)
}

// PreGroup clusters changed files using heuristic rules.
// This is Phase 1 (local, instant) before AI refinement.
//
// Rules applied in order:
//  1. Same directory/package -> grouped together
//  2. Name affinity (foo.go + foo_test.go) -> merged into same group
//  3. File type clustering (configs together, docs together)
//  4. Singleton fallback for unmatched files
func PreGroup(changeset watcher.ChangeSet) []FileGroup {
	// TODO: Implement heuristic grouping
	//
	// Step 1: Group by directory
	// Step 2: Merge groups with name affinity (test files with their source)
	// Step 3: Cluster remaining singletons by file type
	// Step 4: Return final groups

	if len(changeset.Files) == 0 {
		return nil
	}

	// set 1, files in the same directory/package
	dirGroups := make(map[string][]string)

	// hashmap dir -> group of files
	for _, fc := range changeset.Files {
		dir := filepath.Dir(fc.Path)
		dirGroups[dir] = append(dirGroups[dir], fc.Path)
	}

	// set 2, name affinity

	merged := make(map[string]bool)
	affinityDirs := make(map[string]bool) // dirs that have at least one affinity match

	for dir, files := range dirGroups {
		bases := make(map[string]bool)

		for _, f := range files {
			name := filepath.Base(f)
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			stem = strings.TrimSuffix(stem, "_test")
			bases[stem] = true
		}

		for _, f := range files {
			name := filepath.Base(f)
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			if strings.HasSuffix(stem, "_test") {
				sourceStem := strings.TrimSuffix(stem, "_test")
				if bases[sourceStem] {
					merged[f] = true
					affinityDirs[dir] = true
				}
			}
		}
	}

	var groups []FileGroup

	for dir, files := range dirGroups {
		if len(files) > 1 || merged[files[0]] {
			reason := "same package: " + dir
			if affinityDirs[dir] {
				reason = "name affinity: " + dir
			}
			groups = append(groups, FileGroup{
				Files:  files,
				Reason: reason,
			})
		}
	}

	grouped := make(map[string]bool)

	for _, g := range groups {
		for _, f := range g.Files {
			grouped[f] = true
		}
	}

	for _, fc := range changeset.Files {
		if !grouped[fc.Path] {
			groups = append(groups, FileGroup{
				Files:  []string{fc.Path},
				Reason: "singletons " + filepath.Base(fc.Path),
			})
		}
	}

	return groups
}
