package engine

import (
	"github.com/firasastwani/gitpulse/internal/ai"
	"github.com/firasastwani/gitpulse/internal/config"
	"github.com/firasastwani/gitpulse/internal/git"
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
			e.processChanges(changeset)
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

// processChanges handles a debounced batch of file changes through the full pipeline.
func (e *Engine) processChanges(changeset watcher.ChangeSet) {
	// TODO: Implement the full pipeline:
	// 1. Log change detection
	// 2. Get diffs for each file
	// 3. PreGroup with heuristics
	// 4. AI refine groups + generate commit messages
	// 5. For each group: stage, commit
	// 6. Push all commits
	// 7. Save to store
	_ = changeset
}
