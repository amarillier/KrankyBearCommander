// autorefresh.go — live-updates a pane's active tab when its directory
// changes on disk, so a file dropped in by another program (a camera-card
// import, a sync client, ...) shows up without an explicit Refresh click —
// TotalCmd's own long-standing convenience.
//
// Deliberately narrow in scope to keep this free of real resource overhead:
// only the ACTIVE tab of each pane is watched (at most two directories at a
// time, total, across the whole app), and only when that tab's backend is
// the local filesystem (internal/vfs/localfs). fsnotify wraps the OS's own
// inotify/FSEvents/ReadDirectoryChangesW, so a watch is one held OS handle
// and zero polling. Remote backends (SFTP/SMB/FileAgent) are protocol
// clients with no filesystem path to hand the OS, and archive tabs (zipfs)
// browse a snapshot that can't change under you — those tabs fall back to
// the pane's existing manual Refresh button exactly as before.
package main

import (
	"log"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"github.com/fsnotify/fsnotify"

	"commander/internal/vfs/localfs"
)

// autoRefreshDebounce coalesces a burst of filesystem events (e.g. hundreds
// of files landing from a copy or archive extraction) into a single Reload,
// rather than re-reading the directory on every individual event.
const autoRefreshDebounce = 400 * time.Millisecond

// autoRefresher watches one pane's active-tab directory and reloads it on
// change. One instance per pane, created in newPane and stopped from
// commander's quit teardown (see CLAUDE.md's ticker-goroutine pattern: a
// done channel + sync.Once so stop() is safe even if called more than
// once). A nil *autoRefresher (fsnotify unavailable on this platform/setup)
// is valid and simply disables live-updates — every method tolerates it.
type autoRefresher struct {
	watcher *fsnotify.Watcher
	done    chan struct{}
	once    sync.Once

	mu          sync.Mutex
	watchedPath string
	timer       *time.Timer

	// onChange is called (already wrapped in fyne.Do internally) after a
	// debounced burst of changes to whichever directory is currently
	// watched — the pane points this at "reload my active tab" rather than
	// this type knowing about panes/views at all.
	onChange func()
}

func newAutoRefresher(onChange func()) *autoRefresher {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		// Not fatal — the pane's manual Refresh button still works exactly
		// as it always has, just without live-updates.
		log.Printf("auto-refresh unavailable: %v", err)
		return nil
	}
	ar := &autoRefresher{watcher: w, done: make(chan struct{}), onChange: onChange}
	go ar.run()
	return ar
}

func (ar *autoRefresher) run() {
	for {
		select {
		case _, ok := <-ar.watcher.Events:
			if !ok {
				return
			}
			ar.scheduleReload()
		case _, ok := <-ar.watcher.Errors:
			if !ok {
				return
			}
			// Best-effort: e.g. the watched directory itself was removed.
			// The pane's own navigation/Reload logic already handles a
			// vanished directory; this just means no more live-updates
			// for it until the tab navigates elsewhere.
		case <-ar.done:
			return
		}
	}
}

func (ar *autoRefresher) scheduleReload() {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	if ar.timer != nil {
		ar.timer.Stop()
	}
	ar.timer = time.AfterFunc(autoRefreshDebounce, func() { fyne.Do(ar.onChange) })
}

// setPath updates which directory is watched — a no-op if it's already the
// one being watched, and pass "" to stop watching (a non-local tab became
// active, or the pane has no active tab right now).
func (ar *autoRefresher) setPath(path string) {
	if ar == nil {
		return
	}
	ar.mu.Lock()
	defer ar.mu.Unlock()
	if path == ar.watchedPath {
		return
	}
	if ar.watchedPath != "" {
		ar.watcher.Remove(ar.watchedPath)
	}
	ar.watchedPath = ""
	if path != "" {
		if err := ar.watcher.Add(path); err == nil {
			ar.watchedPath = path
		}
		// Add can fail (permission denied, an unusual filesystem) — that
		// just means no live-updates for this directory, same as any other
		// backend this feature doesn't cover.
	}
}

func (ar *autoRefresher) stop() {
	if ar == nil {
		return
	}
	ar.once.Do(func() {
		close(ar.done)
		ar.watcher.Close()
	})
}

// syncAutoRefreshWatch points p's auto-refresher at its active tab's
// directory if that tab browses the local filesystem, or stops watching
// otherwise. Called from refreshChrome, which already runs on every event
// that can change what a pane's active tab is or where it points (tab
// select, navigation, tab add/remove/duplicate/move).
func (p *pane) syncAutoRefreshWatch() {
	if p.autoRefresh == nil {
		return
	}
	view := p.activeView()
	if view == nil {
		p.autoRefresh.setPath("")
		return
	}
	if _, local := view.fs.(*localfs.FS); !local {
		p.autoRefresh.setPath("")
		return
	}
	p.autoRefresh.setPath(view.state.Path)
}
