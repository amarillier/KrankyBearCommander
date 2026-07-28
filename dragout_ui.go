// dragout_ui.go — wires the native drag-out mechanism (internal/dragout,
// macOS only for now) to this app's actual panes. Fyne has no portable API
// for INITIATING an OS-level drag (see dragdrop_ui.go for the drag-IN
// direction, which does have one), so this bridges Fyne's real native
// window handle into platform-specific code; here we just decide, given
// where a click-drag started, what files it should carry.
package main

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"commander/internal/dragout"
	"commander/internal/vfs/zipfs"
)

// installDragOut wires up native drag-out support once the main window is
// showing — must be called after win.Show() (see dragout.Install). A no-op
// on platforms without an implementation (dragout.Supported() false).
func (c *commander) installDragOut() {
	if !dragout.Supported() {
		return
	}
	if err := dragout.Install(c.win, c.dragoutHitTest); err != nil {
		log.Println("drag-out not available:", err)
	}
}

// dragoutHitTest decides what a click-drag starting at pos (window-content
// coordinates, top-left origin — the same convention Window.SetOnDropped
// already uses) should carry out to Finder/Explorer/Nautilus: whatever's
// currently selected (or the cursor row) in whichever pane the drag
// started in — the same "selection or cursor" rule every other bulk
// operation uses (Copy, Multi-Rename, etc. — see
// fileListView.SelectionOrCursor). Returns nil to leave an ordinary click
// alone: nothing to drag if the pane is browsing a read-only archive
// (those aren't real files on disk), nothing is selected/cursored, the
// drag started outside the row list itself (toolbar/tabs/status bar), or
// it started on the vertical scrollbar — found the hard way: dragging the
// scrollbar thumb was being misread as a file drag, since the row list's
// own bounding box includes the scrollbar strip along its right edge.
//
// Also nil whenever any dialog/popup menu is open (found the same way, via
// the Search dialog's own results-list scrollbar): the native drag-out
// monitor (dragout_darwin.go's NSEvent monitor, dragout_windows.go's window
// subclass) watches the whole native window at the OS level, unaware that a
// Fyne dialog is a same-window overlay covering part of it — so without
// this check, a drag that starts on/over a dialog sitting on top of a pane
// still hit-tests against whatever pane is underneath.
func (c *commander) dragoutHitTest(pos fyne.Position) []string {
	if c.win.Canvas().Overlays().Top() != nil {
		return nil
	}

	p := c.paneAt(pos)
	view := p.activeView()
	if view == nil {
		return nil
	}
	if _, ok := view.fs.(*zipfs.FS); ok {
		return nil
	}

	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(view.root)
	size := view.root.Size()
	if pos.X < origin.X || pos.X > origin.X+size.Width-theme.ScrollBarSize() ||
		pos.Y < origin.Y || pos.Y > origin.Y+size.Height {
		return nil
	}

	return view.SelectionOrCursor()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
