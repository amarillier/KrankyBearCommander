// clipboard_ui.go — Ctrl/Cmd+C copies the selection to the OS clipboard as
// real files (not just their name/path as text — see contextmenu_ui.go's
// Copy Name/Copy Path for that), so Finder/Explorer/Nautilus can Paste them
// as actual files; Ctrl/Cmd+V does the reverse, copying whatever files are
// currently on the OS clipboard into the active pane's directory. See
// internal/osclipboard for why this is native platform code rather than
// osascript/PowerShell.
package main

import (
	"commander/internal/fsops"
	"commander/internal/osclipboard"
)

// copyToClipboard puts view's selection (or cursor row) on the OS clipboard
// as real files. Takes view explicitly rather than resolving it from
// c.activePane() — right-clicking a row (see contextmenu_ui.go's "Copy")
// doesn't make that row's pane the active one, so this must act on
// whichever view the caller actually means, not a guess.
func (c *commander) copyToClipboard(view *fileListView) {
	if view == nil || c.blockIfArchive(view) {
		return
	}
	// The OS clipboard only ever holds real local file references — a
	// remote connection's presented paths aren't files Finder/Explorer
	// could actually paste.
	if _, ok := view.fs.(remoteConnFS); ok {
		c.showStatus("copying a remote file to the OS clipboard isn't supported yet — use F5 Copy to download it first")
		return
	}
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("nothing to copy")
		return
	}
	if err := osclipboard.CopyFiles(paths); err != nil {
		c.showStatus("cannot copy to clipboard: " + err.Error())
	}
}

// doCopyToClipboard is Ctrl/Cmd+C: puts the active pane's selection (or
// cursor row) on the OS clipboard as real files.
func (c *commander) doCopyToClipboard() {
	c.copyToClipboard(c.activePane().activeView())
}

// pasteInto copies whatever files are currently on the OS clipboard (e.g.
// from Ctrl+C in Finder/Explorer, or copyToClipboard above) into view's
// current directory, reusing the same progress-dialog/conflict-resolution
// machinery as F5 Copy. Takes p/view explicitly for the same reason
// copyToClipboard does — a right-click "Paste" (see contextmenu_ui.go)
// pastes into whichever pane/tab was right-clicked, not necessarily the
// active one.
func (c *commander) pasteInto(p *pane, view *fileListView) {
	if view == nil || c.blockIfArchive(view) || c.blockIfListbox(view) {
		return
	}
	paths, err := osclipboard.PasteFiles()
	if err != nil {
		c.showStatus("cannot read clipboard: " + err.Error())
		return
	}
	if len(paths) == 0 {
		c.showStatus("clipboard has no files to paste")
		return
	}
	// The OS clipboard only ever holds real local file references, so the
	// source side here is always real — only the destination might be a
	// remote connection, needing Upload instead of fsops.Copy's raw os.*
	// calls.
	op := fsOpFunc(fsops.Copy)
	if remoteFS, ok := view.fs.(remoteConnFS); ok {
		op = remoteFS.Upload
	}
	c.runFileOp("Pasting", paths, view.CurrentPath(), op, p)
}

// doPaste is Ctrl/Cmd+V: pastes into the active pane's current directory.
func (c *commander) doPaste() {
	p := c.activePane()
	c.pasteInto(p, p.activeView())
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
