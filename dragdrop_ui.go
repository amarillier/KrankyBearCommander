// dragdrop_ui.go — drag files from Finder/Explorer/Nautilus and drop them
// onto a pane to copy them in. Fyne has a real, portable API for this
// direction (Window.SetOnDropped, wired in main.go); no native/cgo code
// needed, unlike dragging FROM commander out to another app — Fyne has no
// equivalent API for initiating an OS-level drag, so that direction is
// deferred (see ReleaseNotes.txt's Later phase).
package main

import (
	"fyne.io/fyne/v2"

	"commander/internal/fsops"
)

// handleDropped is Window.SetOnDropped's callback: copies the dropped
// files into whichever pane the drop landed in, at that pane's active
// tab's current directory.
func (c *commander) handleDropped(pos fyne.Position, uris []fyne.URI) {
	if len(uris) == 0 {
		return
	}
	paths := make([]string, 0, len(uris))
	for _, u := range uris {
		if u == nil || u.Scheme() != "file" {
			continue
		}
		paths = append(paths, u.Path())
	}
	if len(paths) == 0 {
		return
	}

	p := c.paneAt(pos)
	view := p.activeView()
	if view == nil {
		return
	}
	if c.blockIfArchive(view) || c.blockIfListbox(view) {
		return
	}
	// Dropped files are always real local paths (from Finder/Explorer/
	// Nautilus) — only the destination might be a remote connection.
	op := fsOpFunc(fsops.Copy)
	if remoteFS, ok := view.fs.(remoteConnFS); ok {
		op = remoteFS.Upload
	}
	c.runFileOp("Copying", paths, view.CurrentPath(), op, p)
}

// paneAt resolves which pane a window-relative position falls into, based
// on the split's current divider — SetOnDropped reports a single
// window-wide position, not a per-widget one, so this is the only way to
// know which side of the split a drop landed on.
func (c *commander) paneAt(pos fyne.Position) *pane {
	if pos.X < c.split.Size().Width*float32(c.split.Offset) {
		return c.left
	}
	return c.right
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
