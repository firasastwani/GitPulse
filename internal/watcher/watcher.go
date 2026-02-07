package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeType represents the kind of file change detected.
type ChangeType int

const (
	Modified ChangeType = iota
	Created
	Deleted
	Renamed
)

// test change comment

// FileChange represents a single file change event.
type FileChange struct {
	Path string
	Type ChangeType
}

// ChangeSet represents a debounced batch of file changes.
type ChangeSet struct {
	Files     []FileChange
	Timestamp time.Time
}

// Watcher monitors a directory tree for file changes and emits debounced ChangeSets.
type Watcher struct {
	root           string
	debounceDelay  time.Duration
	ignorePatterns []string
	events         chan ChangeSet
	done           chan struct{}
}

// New creates a new Watcher for the given path.
// debounceSeconds controls how long to batch raw fsnotify events (keep short, ~2s).
func New(root string, debounceSeconds int, ignorePatterns []string) (*Watcher, error) {
	return &Watcher{
		root:           root,
		debounceDelay:  time.Duration(debounceSeconds) * time.Second,
		ignorePatterns: ignorePatterns,
		events:         make(chan ChangeSet, 10),
		done:           make(chan struct{}),
	}, nil
}

// Events returns the channel that emits debounced ChangeSets.
func (w *Watcher) Events() <-chan ChangeSet {
	return w.events
}

// Start begins watching the directory tree recursively for file changes.
// Returns immediately; the initial directory walk runs asynchronously so startup stays fast.
func (w *Watcher) Start() error {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Event-processing goroutine (runs immediately)
	go func() {
		defer fsWatcher.Close()

		var pending []FileChange
		var timer *time.Timer

		for {
			select {
			case event, ok := <-fsWatcher.Events:
				if !ok {
					return
				}

				if w.shouldIgnore(event.Name) {
					continue
				}

				// Auto-watch newly created directories
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = fsWatcher.Add(event.Name)
						continue
					}
				}

				var changeType ChangeType
				switch {
				case event.Has(fsnotify.Create):
					changeType = Created
				case event.Has(fsnotify.Remove):
					changeType = Deleted
				case event.Has(fsnotify.Write):
					changeType = Modified
				case event.Has(fsnotify.Rename):
					changeType = Renamed
				default:
					continue
				}

				// Store relative path
				relPath, err := filepath.Rel(w.root, event.Name)
				if err != nil {
					relPath = event.Name
				}

				pending = append(pending, FileChange{
					Path: relPath,
					Type: changeType,
				})

				// Short debounce — just batches rapid saves, not the pipeline trigger
				if timer != nil {
					timer.Stop()
				}
				snapshot := make([]FileChange, len(pending))
				copy(snapshot, pending)

				timer = time.AfterFunc(2*time.Second, func() {
					w.events <- ChangeSet{
						Files:     snapshot,
						Timestamp: time.Now(),
					}
					pending = nil
				})

			case _, ok := <-fsWatcher.Errors:
				if !ok {
					return
				}

			case <-w.done:
				if timer != nil {
					timer.Stop()
				}
				return
			}
		}
	}()

	// Walk directory tree asynchronously — don't block startup (Walk can be slow on cloud-synced or large trees)
	go func() {
		_ = filepath.Walk(w.root, func(path string, info os.FileInfo, walkErr error) error {
			select {
			case <-w.done:
				return filepath.SkipAll
			default:
			}
			if walkErr != nil {
				return nil
			}
			if info.IsDir() {
				if w.shouldIgnore(path) {
					return filepath.SkipDir
				}
				_ = fsWatcher.Add(path)
			}
			return nil
		})
	}()

	return nil
}

// shouldIgnore checks if a path matches any configured ignore patterns.
func (w *Watcher) shouldIgnore(path string) bool {
	base := filepath.Base(path)
	for _, pattern := range w.ignorePatterns {
		pattern = strings.TrimSuffix(pattern, "/")
		if base == pattern {
			return true
		}
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.done)
}


// test