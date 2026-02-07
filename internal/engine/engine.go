package engine

// testing stuff........

import (
	"fmt"
	"strings"

	"github.com/firasastwani/gitpulse/internal/ai"
	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/git"
	"github.com/firasastwani/gitpulse/internal/grouper"
	"github.com/firasastwani/gitpulse/internal/store"
	"github.com/firasastwani/gitpulse/internal/ui"
	"github.com/firasastwani/gitpulse/internal/watcher"
)

// Engine orchestrates the full GitPulse pipeline:
// watcher -> grouper -> per-group (stage, AI commit, commit) -> push
type Engine struct {
	cfg     *config.Config
	logger  *ui.Logger
	watcher *watcher.Watcher
	git     *git.Manager
	ai      *ai.Client
	store   *store.Store
	done    chan struct{}
}

// New creates a new Engine with all components wired together.
func New(cfg *config.Config, logger *ui.Logger) (*Engine, error) {
	// Initialize watcher
	w, err := watcher.New(cfg.WatchPath, cfg.DebounceSeconds, cfg.IgnorePatterns)
	if err != nil {
		return nil, err
	}

	// Initialize git manager
	g, err := git.New(cfg.WatchPath, cfg.Remote, cfg.Branch)
	if err != nil {
		return nil, err
	}

	// Initialize AI client
	aiClient := ai.NewClient(cfg.AI.APIKey, cfg.AI.Model)

	// Initialize store
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

// Run starts the main engine loop. Call this in a goroutine.
func (e *Engine) Run() {
	// Start the file watcher
	if err := e.watcher.Start(); err != nil {
		e.logger.Error("Failed to start watcher", err)
		return
	}

	e.logger.Info("Watching for changes...")

	for {
		select {
		case changeset := <-e.watcher.Events():
			e.ProcessChanges(changeset)
		case <-e.done:
			return
		}
	}
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop() {
	e.watcher.Stop()
	close(e.done)
}

// ProcessChanges handles a debounced batch of file changes through the full pipeline.
func (e *Engine) ProcessChanges(changeset watcher.ChangeSet) {
	// TODO: Implement the full pipeline:
	// 1. Log change detection
	// 2. Get diffs for each file
	// 3. PreGroup with heuristics
	// 4. AI refine groups + generate commit messages
	// 5. For each group: stage, commit
	// 6. Push all commits
	// 7. Save to store

	e.logger.Info("Changes detected", "files", len(changeset.Files))

	for _, fc := range changeset.Files {
		e.logger.Info("detected change", "path", fc.Path, "type", fc.Type)
	}

	groups := grouper.PreGroup(changeset)
	e.logger.Info("Pre-grouped files", "groups", len(groups))

	// get diffs
	for i := range groups {

		for _, f := range groups[i].Files {

			d, err := e.git.GetFileDiff(f)
			if err != nil {
				d = fmt.Sprintf("--- /dev/null\n+++ b/%s\n(new or deleted file)", f)
			}
			groups[i].Diffs += d + "\n"
		}
	}

	refined, err := e.ai.RefineAndCommit(groups)

	if err != nil {
		e.logger.Warn("AI refinement failed, falling back to heuristic groups", "err", err)
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

	// 5. Reset staging, then for each group: stage + commit
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

		// 7. Save to store
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

	// 6. Push all commits
	if commitCount > 0 && e.cfg.AutoPush {
		if err := e.git.Push(); err != nil {
			e.logger.Error("Failed to push", err)
			return
		}
		e.logger.PushSuccess(commitCount, e.cfg.Remote)
	}
}
