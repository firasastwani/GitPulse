package engine

import (
	"fmt"
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

	// 4. Reset staging, then stage + commit per group
	if err := e.git.ResetStaging(); err != nil {
		e.logger.Error("Failed to reset staging", err)
		return
	}

	commitCount := 0
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
		commitCount++

		if err := e.store.Save(store.CommitRecord{
			Hash:        hash,
			Message:     g.CommitMessage,
			Files:       g.Files,
			GroupReason: g.Reason,
			AIGenerated: true,
			Pushed:      false,
		}); err != nil {
			e.logger.Warn("Failed to save commit record", "err", err)
		}
	}

	// 5. Push
	if commitCount > 0 && e.cfg.AutoPush {
		if err := e.git.Push(); err != nil {
			e.logger.Error("Failed to push", err)
			return
		}
		e.logger.PushSuccess(commitCount, e.cfg.Remote)
	}
}
