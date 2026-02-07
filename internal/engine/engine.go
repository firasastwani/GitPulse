package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/firasastwani/gitpulse/internal/ai"
	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/git"
	"github.com/firasastwani/gitpulse/internal/grouper"
	"github.com/firasastwani/gitpulse/internal/store"
	"github.com/firasastwani/gitpulse/internal/ui"
	"github.com/firasastwani/gitpulse/internal/watcher"
)

// how many time
const maxReviewIterations = 3

// Engine orchestrates the full GitPulse pipeline:
// watcher buffers changes -> user triggers `gitpulse push` OR safety timer fires
type Engine struct {
	cfg     *config.Config
	logger  *ui.Logger
	watcher *watcher.Watcher
	git     *git.Manager
	ai      *ai.Client
	store   *store.Store
	done    chan struct{}

	// Interactive controls whether the engine can prompt the user.
	// Set to true in daemon mode (user at terminal), false for safety timer auto-flush.
	Interactive bool

	// pending changes buffer (protected by mu)
	mu      sync.Mutex
	pending []watcher.FileChange

	// safety timer — auto-flushes if user forgets
	timerMu     sync.Mutex
	safetyTimer *time.Timer
}

// New creates a new Engine with all components wired together.
func New(cfg *config.Config, logger *ui.Logger) (*Engine, error) {
	w, err := watcher.New(cfg.WatchPath, cfg.DebounceSeconds, cfg.IgnorePatterns)
	if err != nil {
		return nil, err
	}

	g, err := git.New(cfg.WatchPath, cfg.Remote, cfg.Branch)
	if err != nil {
		return nil, err
	}

	aiClient := ai.NewClient(cfg.AI.APIKey, cfg.AI.Model)

	s, err := store.New("")
	if err != nil {
		return nil, err
	}

	return &Engine{
		cfg:     cfg,
		logger:  logger,
		watcher: w,
		git:     g,
		ai:      aiClient,
		store:   s,
		done:    make(chan struct{}),
	}, nil
}

// Run starts the main engine loop. Buffers changes from the watcher.
func (e *Engine) Run() {
	if err := e.watcher.Start(); err != nil {
		e.logger.Error("Failed to start watcher", err)
		return
	}

	e.logger.Info("Watching for changes...", "safety_timer", fmt.Sprintf("%ds", e.cfg.DebounceSeconds))
	e.logger.Info("Run `gitpulse push` in another terminal to commit & push")

	for {
		select {
		case changeset := <-e.watcher.Events():
			e.bufferChanges(changeset)
		case <-e.done:
			return
		}
	}
}

// bufferChanges adds new file changes to pending and resets the safety timer.
func (e *Engine) bufferChanges(changeset watcher.ChangeSet) {
	e.mu.Lock()
	e.pending = append(e.pending, changeset.Files...)
	count := len(e.pending)
	e.mu.Unlock()

	e.logger.Info("Changes buffered", "new", len(changeset.Files), "total_pending", count)

	// Reset safety timer
	e.resetSafetyTimer()
}

// resetSafetyTimer resets (or starts) the safety timer that auto-flushes.
func (e *Engine) resetSafetyTimer() {
	e.timerMu.Lock()
	defer e.timerMu.Unlock()

	if e.safetyTimer != nil {
		e.safetyTimer.Stop()
	}

	delay := time.Duration(e.cfg.DebounceSeconds) * time.Second
	e.safetyTimer = time.AfterFunc(delay, func() {
		e.mu.Lock()
		hasPending := len(e.pending) > 0
		e.mu.Unlock()

		if hasPending {
			e.logger.Warn("Safety timer fired — auto-flushing pending changes")
			e.Flush()
		}
	})
}

// Flush processes all buffered changes through the full pipeline.
// Called by `gitpulse push` (via SIGUSR1) or by the safety timer.
func (e *Engine) Flush() {
	// Grab and clear pending changes
	e.mu.Lock()
	if len(e.pending) == 0 {
		e.mu.Unlock()
		e.logger.Info("Nothing to flush — no pending changes")
		return
	}
	files := make([]watcher.FileChange, len(e.pending))
	copy(files, e.pending)
	e.pending = nil
	e.mu.Unlock()

	// Stop safety timer since we're flushing now
	e.timerMu.Lock()
	if e.safetyTimer != nil {
		e.safetyTimer.Stop()
	}
	e.timerMu.Unlock()

	changeset := watcher.ChangeSet{
		Files:     files,
		Timestamp: time.Now(),
	}

	e.processChanges(changeset)
}

// PendingCount returns the number of buffered file changes.
func (e *Engine) PendingCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pending)
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop() {
	e.timerMu.Lock()
	if e.safetyTimer != nil {
		e.safetyTimer.Stop()
	}
	e.timerMu.Unlock()

	e.watcher.Stop()
	close(e.done)
}

// processChanges runs the full pipeline: group -> AI -> stage -> commit -> push.
func (e *Engine) processChanges(changeset watcher.ChangeSet) {
	e.logger.Info("Processing changes", "files", len(changeset.Files))

	for _, fc := range changeset.Files {
		e.logger.Info("  file", "path", fc.Path, "type", fc.Type)
	}

	// 1. Heuristic grouping
	groups := grouper.PreGroup(changeset)
	e.logger.Info("Pre-grouped files", "groups", len(groups))

	// 2. Get diffs
	for i := range groups {
		for _, f := range groups[i].Files {
			d, err := e.git.GetFileDiff(f)
			if err != nil {
				d = fmt.Sprintf("--- /dev/null\n+++ b/%s\n(new or deleted file)", f)
			}
			groups[i].Diffs += d + "\n"
		}
	}

	// 3. AI refine + commit messages
	refined, err := e.ai.RefineAndCommit(groups)
	if err != nil {
		e.logger.Warn("AI refinement failed, using heuristic groups", "err", err)
		refined = groups
		for i := range refined {
			if refined[i].CommitMessage == "" {
				refined[i].CommitMessage = "chore: auto-commit changes"
			}
		}
	}

	// Log grouping results
	displays := make([]ui.GroupDisplay, len(refined))
	for i, g := range refined {
		displays[i] = ui.GroupDisplay{
			Files:  strings.Join(g.Files, ", "),
			Reason: g.Reason,
		}
	}
	e.logger.GroupInfo(len(refined), displays)

	// 3.5 AI Code Review — hold push if blockers found
	// Track review data for store records
	var reviewRecord *store.ReviewRecord

	if e.cfg.AI.CodeReview {
		if e.Interactive {
			refined, reviewRecord = e.reviewLoopWithRecord(refined)
		} else {
			// Non-interactive (safety timer): review but only log, don't block
			reviewResult, err := e.ai.ReviewCode(refined)
			if err != nil {
				e.logger.Warn("AI review failed, proceeding without review", "err", err)
			} else {
				reviewRecord = &store.ReviewRecord{
					Findings:    convertFindingsForStore(reviewResult.Findings),
					HasBlockers: reviewResult.HasBlockers,
				}
				if reviewResult.HasBlockers {
					e.logger.Warn("AI review found blockers but running non-interactively, proceeding anyway",
						"issues", len(reviewResult.Findings))
					e.logger.ReviewFindings(reviewResult.Findings)
				} else if len(reviewResult.Findings) > 0 {
					e.logger.Info("AI review passed with info-only findings", "issues", len(reviewResult.Findings))
					e.logger.ReviewFindings(reviewResult.Findings)
				} else {
					e.logger.Info("AI review passed — no issues found")
				}
			}
		}
	}

	// 4. Reset staging, then stage + commit per group
	if err := e.git.ResetStaging(); err != nil {
		e.logger.Error("Failed to reset staging", err)
		return
	}

	var commitHashes []string
	for _, g := range refined {
		if err := e.git.StageFiles(g.Files); err != nil {
			e.logger.Error("Failed to stage files", err, "files", g.Files)
			continue
		}

		hash, err := e.git.Commit(g.CommitMessage)
		if err != nil {
			e.logger.Error("Failed to commit", err)
			continue
		}

		e.logger.CommitSuccess(hash, g.CommitMessage)
		commitHashes = append(commitHashes, hash)

		// Build enriched file changes from diffs
		fileChanges := parseDiffStats(g.Diffs, g.Files)

		record := store.CommitRecord{
			Hash:        hash,
			Message:     g.CommitMessage,
			Files:       fileChanges,
			GroupReason: g.Reason,
			AIGenerated: true,
			Review:      reviewRecord,
		}

		if err := e.store.Save(record); err != nil {
			e.logger.Warn("Failed to save commit record", "err", err)
		}
	}

	// 5. Push and mark records as pushed
	if len(commitHashes) > 0 && e.cfg.AutoPush {
		if err := e.git.Push(); err != nil {
			e.logger.Error("Failed to push", err)
			return
		}
		e.logger.PushSuccess(len(commitHashes), e.cfg.Remote)

		if err := e.store.MarkPushed(commitHashes, e.cfg.Remote, e.cfg.Branch); err != nil {
			e.logger.Warn("Failed to mark commits as pushed", "err", err)
		}
	}
}

// reviewLoopWithRecord runs the interactive review cycle and returns the final
// review record for storage alongside the (possibly updated) groups.
func (e *Engine) reviewLoopWithRecord(groups []grouper.FileGroup) ([]grouper.FileGroup, *store.ReviewRecord) {
	var record *store.ReviewRecord

	for iteration := 0; iteration < maxReviewIterations; iteration++ {
		reviewResult, err := e.ai.ReviewCode(groups)
		if err != nil {
			e.logger.Warn("AI review failed, proceeding without review", "err", err)
			return groups, nil
		}

		record = &store.ReviewRecord{
			Findings:    convertFindingsForStore(reviewResult.Findings),
			HasBlockers: reviewResult.HasBlockers,
		}

		if len(reviewResult.Findings) == 0 {
			e.logger.Info("AI review passed — no issues found")
			return groups, record
		}

		// Display findings
		e.logger.ReviewFindings(reviewResult.Findings)

		if !reviewResult.HasBlockers {
			e.logger.Info("All findings are info-only, proceeding with push")
			return groups, record
		}

		// Prompt user for action
		action, err := e.handleReviewFindings(groups, reviewResult)
		if err != nil {
			e.logger.Warn("Review prompt failed, proceeding with push", "err", err)
			return groups, record
		}

		record.Action = action

		if action == "continue" {
			e.logger.Info("User chose to continue — proceeding with push")
			return groups, record
		}

		// Track fixes applied
		if action == "aifix" {
			for _, f := range reviewResult.Findings {
				if f.Severity == ai.SeverityError || f.Severity == ai.SeverityWarning {
					record.FixesApplied = append(record.FixesApplied, store.FixRecord{
						File:        f.File,
						Description: f.Description,
						FixType:     "ai",
					})
				}
			}
		} else if action == "manual" {
			record.FixesApplied = append(record.FixesApplied, store.FixRecord{
				File:        "multiple",
				Description: "User applied manual fixes",
				FixType:     "manual",
			})
		}

		// Re-fetch diffs after fix (manual or AI) for the next iteration
		for i := range groups {
			groups[i].Diffs = ""
			for _, f := range groups[i].Files {
				d, err := e.git.GetFileDiff(f)
				if err != nil {
					d = fmt.Sprintf("--- /dev/null\n+++ b/%s\n(new or deleted file)", f)
				}
				groups[i].Diffs += d + "\n"
			}
		}

		e.logger.Info("Re-reviewing after fix...", "iteration", iteration+2)
	}

	e.logger.Warn("Max review iterations reached, proceeding with push")
	return groups, record
}

// handleReviewFindings prompts the user and executes the chosen action.
// Returns the action string ("manual", "aifix", "continue") and any error.
func (e *Engine) handleReviewFindings(groups []grouper.FileGroup, result *ai.ReviewResult) (string, error) {
	action, err := e.logger.PromptReviewAction()
	if err != nil {
		return "continue", err
	}

	switch action {
	case "manual":
		if err := e.logger.WaitForManualFix(); err != nil {
			return "continue", err
		}

	case "aifix":
		e.applyAIFixes(result.Findings)
	}

	return action, nil
}

// parseDiffStats splits a combined unified diff into per-file FileChange records
// with line-added/removed counts and file status (added, deleted, modified).
func parseDiffStats(combinedDiff string, files []string) []store.FileChange {
	// Build per-file diffs by splitting on "diff --git" boundaries
	fileDiffs := make(map[string]string)
	sections := strings.Split(combinedDiff, "diff --git")
	for _, section := range sections {
		if strings.TrimSpace(section) == "" {
			continue
		}
		section = "diff --git" + section
		for _, f := range files {
			if strings.Contains(section, " a/"+f) || strings.Contains(section, " b/"+f) {
				fileDiffs[f] = section
				break
			}
		}
	}

	changes := make([]store.FileChange, 0, len(files))
	for _, f := range files {
		diff := fileDiffs[f]

		// Count added/removed lines (skip diff headers starting with @@, ---, +++)
		var added, removed int
		for _, line := range strings.Split(diff, "\n") {
			if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
				continue
			}
			if strings.HasPrefix(line, "+") {
				added++
			} else if strings.HasPrefix(line, "-") {
				removed++
			}
		}

		// Determine file status
		status := "modified"
		if strings.Contains(diff, "--- /dev/null") {
			status = "added"
		} else if strings.Contains(diff, "+++ /dev/null") {
			status = "deleted"
		}

		changes = append(changes, store.FileChange{
			Path:         f,
			Diff:         diff,
			LinesAdded:   added,
			LinesRemoved: removed,
			Status:       status,
		})
	}

	// Handle files with no diff section (e.g., new untracked files)
	seen := make(map[string]bool)
	for _, c := range changes {
		seen[c.Path] = true
	}
	for _, f := range files {
		if !seen[f] {
			changes = append(changes, store.FileChange{
				Path:   f,
				Status: "added",
			})
		}
	}

	return changes
}

// convertFindingsForStore converts ai.ReviewFinding to store.ReviewFinding
// to avoid import cycles between the store and ai packages.
func convertFindingsForStore(findings []ai.ReviewFinding) []store.ReviewFinding {
	result := make([]store.ReviewFinding, len(findings))
	for i, f := range findings {
		result[i] = store.ReviewFinding{
			File:        f.File,
			StartLine:   f.StartLine,
			EndLine:     f.EndLine,
			Severity:    f.Severity,
			Description: f.Description,
			Suggestion:  f.Suggestion,
		}
	}
	return result
}

// applyAIFixes iterates through blocking findings and applies AI-generated fixes.
func (e *Engine) applyAIFixes(findings []ai.ReviewFinding) {
	for _, finding := range findings {
		// Only fix blockers
		if finding.Severity != ai.SeverityError && finding.Severity != ai.SeverityWarning {
			continue
		}

		// Read the primary file content
		absPath := filepath.Join(e.cfg.WatchPath, finding.File)
		primaryBytes, err := os.ReadFile(absPath)
		if err != nil {
			e.logger.Warn("Could not read file for AI fix", "file", finding.File, "err", err)
			continue
		}

		// Read related file contents for cross-file context
		relatedContents := make(map[string]string)
		for _, loc := range finding.RelatedLocations {
			relPath := filepath.Join(e.cfg.WatchPath, loc.File)
			relBytes, err := os.ReadFile(relPath)
			if err != nil {
				continue // skip related files we can't read
			}
			relatedContents[loc.File] = string(relBytes)
		}

		// Ask AI to generate the fix
		fixed, err := e.ai.GenerateFix(finding.File, finding, string(primaryBytes), relatedContents)
		if err != nil {
			e.logger.Warn("AI fix generation failed", "file", finding.File, "err", err)
			continue
		}

		// Write the fix back to disk
		if err := os.WriteFile(absPath, []byte(fixed), 0644); err != nil {
			e.logger.Warn("Failed to write AI fix", "file", finding.File, "err", err)
			continue
		}

		e.logger.AIFixApplied(finding.File, finding.Description)
	}
}
