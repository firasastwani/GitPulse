package watcher

import (
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

// FileChange represents a single file change event.
type FileChange struct {
	Path string
	Type ChangeType
}

// ChangeSet represents a debounced(does after a period of inactivity) batch of file changes.
type ChangeSet struct {
	Files     []FileChange
	Timestamp time.Time
}

// Watcher monitors a directory for file changes and emits debounced ChangeSets.
type Watcher struct {
	path           string
	debounceDelay  time.Duration
	ignorePatterns []string
	events         chan ChangeSet
	done           chan struct{}
}

// New creates a new Watcher for the given path.
func New(path string, debounceSeconds int, ignorePatterns []string) (*Watcher, error) {
	// TODO: Initialize fsnotify watcher, set up debounce timer
	return &Watcher{
		path:           path,
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

// Start begins watching for file changes.
func (w *Watcher) Start() error {

	// create a new watcher
	fsWatcher, err := fsnotify.NewWatcher()

	if err != nil {
		return err
	}

	err = fsWatcher.Add(w.path)

	if err != nil {
		fsWatcher.Close()
		return err
	}

	go func() {

		defer fsWatcher.Close()

		// changes
		var pending []FileChange
		// debounce increment
		var timer *time.Timer

		for {
			select {
			case event, ok := <-fsWatcher.Events:

				if !ok {
					return
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

				pending = append(pending, FileChange{
					Path: event.Name,
					Type: changeType,
				})

				if timer != nil {
					timer.Stop()
				}

				// capture snapshot to avoid data race — AfterFunc runs on another goroutine
				snapshot := make([]FileChange, len(pending))
				copy(snapshot, pending)

				timer = time.AfterFunc(w.debounceDelay, func() {
					w.events <- ChangeSet{
						Files:     snapshot,
						Timestamp: time.Now(),
					}
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

	return nil
}

// Stop shuts down the watcher (ctrl c)
func (w *Watcher) Stop() {
	close(w.done)
}
