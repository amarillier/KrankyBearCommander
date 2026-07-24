// rename_ui.go — inline click-to-rename: click an already-selected/cursor
// row's name again (not fast enough to count as a double-click) to edit its
// name in place, in either view mode. Enter commits, Escape or clicking
// away cancels. The extension is hidden during editing rather than
// selected-but-preserved: Fyne's Entry has no public API for selecting a
// sub-range of its text (only select-all), so editing just the base name
// and silently reattaching the original extension on commit sidesteps that
// limitation entirely, while still preventing an accidental extension edit.
package main

import (
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/vfs"
	"commander/internal/vfs/zipfs"
)

// renamingState tracks which row is currently being edited in place.
type renamingState struct {
	name string // the entry's real current name
	ext  string // re-attached on commit if the typed text doesn't include a "."
}

// stemExt splits name into an editable base and an extension to preserve —
// directories, and files with no extension (or dotfiles like ".gitignore",
// which filepath.Ext would otherwise treat as entirely "extension"), just
// get their whole name treated as the editable base.
func stemExt(name string, isDir bool) (stem, ext string) {
	if isDir {
		return name, ""
	}
	ext = filepath.Ext(name)
	stem = strings.TrimSuffix(name, ext)
	if stem == "" {
		return name, ""
	}
	return stem, ext
}

// beginInlineRename starts editing entry's name in place — triggered by a
// second, slow (non-double-click) tap on a row that was already the cursor
// (see handleTableTap and buildBriefCell's tap handlers).
func (v *fileListView) beginInlineRename(entry vfs.Entry) {
	if entry.Name == parentEntryName {
		return
	}
	if _, insideArchive := v.fs.(*zipfs.FS); insideArchive {
		return // nothing to rename inside a read-only archive
	}
	stem, ext := stemExt(entry.Name, entry.IsDir)
	v.renaming = &renamingState{name: entry.Name, ext: ext}
	v.activeRenameField = nil
	v.Refresh() // synchronously re-renders, populating v.activeRenameField below

	if v.activeRenameField == nil {
		return
	}
	v.activeRenameField.committed = false // this cell's renameEntry is reused across sessions — see its doc comment
	v.activeRenameField.SetText(stem)
	if canvas := fyne.CurrentApp().Driver().CanvasForObject(v.activeRenameField); canvas != nil {
		canvas.Focus(v.activeRenameField)
	}
	v.activeRenameField.TypedShortcut(&fyne.ShortcutSelectAll{})
}

// commitRename is the rename entry's Enter handler. An unchanged or blank
// name is treated as a cancel; a typed name with no "." gets the original
// extension reattached, so the common case (just retyping the base name)
// can't accidentally drop it.
func (v *fileListView) commitRename(text string) {
	r := v.renaming
	if r == nil {
		return
	}
	v.renaming = nil
	v.activeRenameField = nil

	newName := strings.TrimSpace(text)
	if newName == "" || newName == strings.TrimSuffix(r.name, r.ext) {
		v.Refresh()
		return
	}
	if r.ext != "" && !strings.Contains(newName, ".") {
		newName += r.ext
	}
	if strings.ContainsAny(newName, "/\\") {
		if v.onStatus != nil {
			v.onStatus("a name can't contain a path separator")
		}
		v.Refresh()
		return
	}

	oldPath := v.fs.Join(v.state.Path, r.name)
	newPath := v.fs.Join(v.state.Path, newName)
	if err := fsops.Rename(oldPath, newPath); err != nil {
		if v.onStatus != nil {
			v.onStatus("cannot rename: " + err.Error())
		}
	}
	v.Reload()
}

// cancelRename is the rename entry's Escape/focus-lost handler.
func (v *fileListView) cancelRename() {
	if v.renaming == nil {
		return
	}
	v.renaming = nil
	v.activeRenameField = nil
	v.Refresh()
}

// forceCancelRename cancels any in-progress rename immediately, called
// directly from handleTableTap/buildBriefCell's tap handlers the moment any
// OTHER row is clicked — rather than only trusting the rename entry's own
// FocusLost to fire in time. widget.Table's Tapped grabs focus for ITSELF
// as part of handling that very click (see keyTable's doc comment), and in
// practice that hasn't reliably blurred the rename entry first every time,
// leaving it stuck open. committed is marked here too, so a FocusLost that
// does eventually arrive late for this same entry is a no-op rather than
// double-canceling (or, worse, canceling whatever new rename may have
// started in the meantime).
func (v *fileListView) forceCancelRename() {
	if v.renaming == nil {
		return
	}
	if v.activeRenameField != nil {
		v.activeRenameField.committed = true
	}
	v.renaming = nil
	v.activeRenameField = nil
	v.Refresh()
}

// renameEntry extends widget.Entry purely to add the two hooks Fyne's Entry
// doesn't expose publicly: Escape-to-cancel (TypedKey has no Escape case at
// all) and cancel-on-blur (FocusLost's own onFocusChanged callback is
// unexported) — the same "extend the widget ourselves" pattern keyTable and
// tappableCell already use elsewhere in this file for the same reason.
type renameEntry struct {
	widget.Entry
	onCommit  func(text string)
	onCancel  func()
	committed bool // guards against both Enter and the FocusLost it causes firing
}

func newRenameEntry(onCommit func(string), onCancel func()) *renameEntry {
	e := &renameEntry{onCommit: onCommit, onCancel: onCancel}
	e.ExtendBaseWidget(e)
	e.OnSubmitted = func(text string) { e.commit(text) }
	return e
}

// commit/cancel defer their actual work via fyne.Do rather than running it
// inline. FocusLost in particular can fire synchronously from INSIDE
// widget.Table's own Tapped handler — clicking a different row calls
// canvas.Focus(table) before the table processes that click at all (see
// keyTable's doc comment), which blurs whatever had focus first — so
// cancel() must not go straight on to mutate/Refresh the very table that's
// still partway through handling that click. committed is still set
// synchronously so a fast second trigger (e.g. Enter immediately followed
// by the blur it causes) can't double-fire before the deferred call runs.
func (e *renameEntry) commit(text string) {
	if e.committed {
		return
	}
	e.committed = true
	if e.onCommit != nil {
		fyne.Do(func() { e.onCommit(text) })
	}
}

func (e *renameEntry) cancel() {
	if e.committed {
		return
	}
	e.committed = true
	if e.onCancel != nil {
		fyne.Do(func() { e.onCancel() })
	}
}

func (e *renameEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		e.cancel()
		return
	}
	e.Entry.TypedKey(ev)
}

func (e *renameEntry) FocusLost() {
	e.Entry.FocusLost()
	e.cancel()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
