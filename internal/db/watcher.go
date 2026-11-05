package db

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// stateWatcher watches for changes to the state file
type stateWatcher struct {
	watcher   *fsnotify.Watcher
	events    chan struct{}
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

// WatchForChanges creates a watcher for state file changes
// Returns a stateWatcher that sends notifications when the state file is modified
func (bb *Blackboard) WatchForChanges() (*stateWatcher, error) {
	// Watch the directory containing the state file
	// We watch the directory because atomic renames (used in Write/Modify)
	// create new inodes, and watching the file directly would miss those changes
	watcher, err := watchDir(filepath.Dir(bb.statePath), (*fsnotify.Watcher).Add)
	if err != nil {
		return nil, err
	}

	sw := &stateWatcher{
		watcher: watcher,
		events:  make(chan struct{}, 1), // Buffered to coalesce multiple events
		errors:  make(chan error, 1),
		done:    make(chan struct{}),
	}

	// Start event processing goroutine
	go sw.processEvents(bb.statePath)

	return sw, nil
}

// stateWatchAddAttempts bounds watchDir's retries.
const stateWatchAddAttempts = 5

// watchDir returns a watcher on dir. On kqueue (macOS, BSD) fsnotify's Add
// lists dir and lstats each entry, so an entry renamed away in between —
// filelock's owner-metadata temp file lives beside state.yaml — fails the
// whole Add with ErrNotExist, a transient failure while dir itself exists.
// A failed Add leaves the watcher partly registered, so each attempt starts
// from a new one.
func watchDir(dir string, add func(*fsnotify.Watcher, string) error) (*fsnotify.Watcher, error) {
	var err error
	for range stateWatchAddAttempts {
		watcher, newErr := fsnotify.NewWatcher()
		if newErr != nil {
			return nil, newErr
		}
		if err = add(watcher, dir); err == nil {
			return watcher, nil
		}
		_ = watcher.Close()
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if _, statErr := os.Stat(dir); statErr != nil {
			return nil, err
		}
	}
	return nil, err
}

// stateWatchDebounce coalesces a burst of state-file events into one.
const stateWatchDebounce = 50 * time.Millisecond

// processEvents handles fsnotify events and coalesces them
func (sw *stateWatcher) processEvents(statePath string) {
	defer close(sw.events)
	defer close(sw.errors)

	// Debounce timer to coalesce rapid events
	var debounceTimer *time.Timer
	debounceDuration := stateWatchDebounce

	for {
		select {
		case <-sw.done:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-sw.watcher.Events:
			if !ok {
				return
			}

			// Only care about events for our specific state file
			if event.Name != statePath {
				continue
			}

			// Only care about write/create/rename events (not chmod)
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			// Debounce: reset timer on each event
			if debounceTimer != nil {
				debounceTimer.Stop()
			}

			debounceTimer = time.AfterFunc(debounceDuration, func() {
				// Check closed flag under mutex to prevent send on closed channel.
				// Close() sets closed=true before closing sw.done, so this
				// synchronizes with processEvents' exit path.
				sw.mu.Lock()
				if sw.closed {
					sw.mu.Unlock()
					return
				}
				// Send notification (non-blocking due to buffered channel)
				select {
				case sw.events <- struct{}{}:
				default:
					// Channel already has a pending notification, skip
				}
				sw.mu.Unlock()
			})

		case err, ok := <-sw.watcher.Errors:
			if !ok {
				return
			}

			// Send error (non-blocking)
			select {
			case sw.errors <- err:
			default:
				// Error channel full, skip
			}
		}
	}
}

// Events returns a channel that receives notifications when the state file changes
// The channel is closed when the watcher is closed
func (sw *stateWatcher) Events() <-chan struct{} {
	return sw.events
}

// Errors returns a channel that receives watcher errors
func (sw *stateWatcher) Errors() <-chan error {
	return sw.errors
}

// Close stops the watcher and releases resources
// It's safe to call Close multiple times
func (sw *stateWatcher) Close() error {
	var err error
	sw.closeOnce.Do(func() {
		sw.mu.Lock()
		sw.closed = true
		sw.mu.Unlock()

		close(sw.done)
		err = sw.watcher.Close()
	})
	return err
}
