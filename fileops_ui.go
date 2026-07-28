// fileops_ui.go — F5/F6/F7/F8/Shift+F8 file operations: confirm/prompt
// dialogs, a progress dialog, and the conflict (Overwrite/Skip/Rename/
// Cancel) dialog, all wired to internal/fsops (which does the actual work
// and knows nothing about Fyne). Copy/Move always target the *other* pane's
// current directory, matching classic dual-pane commander behavior.
package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/vfs/zipfs"
)

func (c *commander) inactivePaneOf(p *pane) *pane {
	if p == c.left {
		return c.right
	}
	return c.left
}

// blockIfArchive reports (and refuses) any operation that would need to
// mutate an open archive in place — F4 Edit, Move/Rename, MkDir, Delete.
// Only F5 Copy (repurposed as Extract, see doCopy) and F3 View (see
// viewer.go) make sense while browsing one. A modal rather than the status
// bar: the status bar sits at the bottom of the whole window competing with
// the per-pane status line right above it, easy to miss, and — unlike a
// dialog the user dismisses — it has no natural moment to clear itself, so
// it would otherwise sit there looking current after refreshing, switching
// tabs, or navigating back out of the archive entirely.
func (c *commander) blockIfArchive(view *fileListView) bool {
	if _, ok := view.fs.(*zipfs.FS); ok {
		dialog.ShowInformation("Read-Only Archive", "This archive is read-only — extract with F5 to make changes.", c.win)
		return true
	}
	return false
}

// ── F2 Refresh ───────────────────────────────────────────────────────────────

// doRefresh re-reads both panes' active tabs from disk and re-scans both
// drive bars — F2, Ctrl+R, and every pane's own ⟳ drive-bar button all
// trigger this same "refresh everything" action rather than just whichever
// pane you happened to be in: a partial refresh was confusingly easy to
// misread as "nothing changed" on the other side, and refreshing both costs
// no noticeable time either way.
func (c *commander) doRefresh() {
	for _, p := range []*pane{c.left, c.right} {
		if v := p.activeView(); v != nil {
			v.Reload()
		}
		p.rescanDriveBar()
	}
}

// ── F5 Copy ──────────────────────────────────────────────────────────────────

func (c *commander) doCopy() {
	src := c.activePane()
	view := src.activeView()
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("nothing to copy")
		return
	}
	dst := c.inactivePaneOf(src).activeState()
	if dst == nil {
		return
	}
	target := dst.Path

	// Browsing inside an archive: F5 extracts instead of copying — there's
	// nothing real at these paths for fsops.Copy's os.* calls to find.
	if zfs, ok := view.fs.(*zipfs.FS); ok {
		showDialog(dialog.NewConfirm("Extract", fmt.Sprintf("Extract %d item(s) to:\n%s", len(paths), target), func(ok bool) {
			if !ok {
				return
			}
			c.runFileOp("Extracting", paths, target, zfs.Extract, src)
		}, c.win))
		return
	}

	showDialog(dialog.NewConfirm("Copy", fmt.Sprintf("Copy %d item(s) to:\n%s", len(paths), target), func(ok bool) {
		if !ok {
			return
		}
		c.runFileOp("Copying", paths, target, fsops.Copy, src)
	}, c.win))
}

// ── F6 Move / Rename ─────────────────────────────────────────────────────────
//
// A single source shows an editable full-path dialog (rename in place, move
// elsewhere, or both — classic MC behavior). Multiple sources just move to
// the other pane's current directory, since renaming several files to one
// typed name has no sensible meaning.

func (c *commander) doMoveOrRename() {
	src := c.activePane()
	if c.blockIfArchive(src.activeView()) {
		return
	}
	paths := src.activeView().SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("nothing to move")
		return
	}
	dst := c.inactivePaneOf(src).activeState()
	if dst == nil {
		return
	}

	if len(paths) == 1 {
		oldPath := paths[0]
		nameEntry := newDialogEntry()
		nameEntry.SetText(filepath.Join(dst.Path, filepath.Base(oldPath)))
		content := container.NewVBox(widget.NewLabel("Rename/Move to:"), nameEntry)
		d := dialog.NewCustomConfirm("Rename / Move", "OK", "Cancel", content, func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			c.performRename(oldPath, nameEntry.Text, src)
		}, c.win)
		d.Resize(fyne.NewSize(560, 160))
		showDialog(d)
		c.win.Canvas().Focus(nameEntry)
		return
	}

	target := dst.Path
	showDialog(dialog.NewConfirm("Move", fmt.Sprintf("Move %d item(s) to:\n%s", len(paths), target), func(ok bool) {
		if !ok {
			return
		}
		c.runFileOp("Moving", paths, target, fsops.Move, src)
	}, c.win))
}

func (c *commander) performRename(oldPath, newPath string, sourcePane *pane) {
	if err := fsops.Rename(oldPath, newPath); err != nil {
		// Likely a cross-device rename; fall back to copy+delete into the
		// target directory (the MVP fallback keeps the original name — a
		// simultaneous cross-device rename isn't supported here).
		c.runFileOp("Moving", []string{oldPath}, filepath.Dir(newPath), fsops.Move, sourcePane)
		return
	}
	sourcePane.activeView().Reload()
	c.inactivePaneOf(sourcePane).activeView().Reload()
}

// ── F7 MkDir ─────────────────────────────────────────────────────────────────

func (c *commander) doMkdir() {
	p := c.activePane()
	if c.blockIfArchive(p.activeView()) {
		return
	}
	state := p.activeState()
	if state == nil {
		return
	}
	nameEntry := newDialogEntry()
	nameEntry.SetPlaceHolder("New folder name")
	// Prefill with the cursor row's name (classic commander muscle memory:
	// F7 right after landing on/near a similarly-named item) so the user can
	// either type straight over it or just tweak part of it.
	if def := state.Cursor; def != "" && def != parentEntryName {
		nameEntry.SetText(def)
		nameEntry.CursorColumn = len([]rune(def))
	}
	d := dialog.NewCustomConfirm("New Folder", "Create", "Cancel", nameEntry, func(ok bool) {
		if !ok || strings.TrimSpace(nameEntry.Text) == "" {
			return
		}
		target := filepath.Join(state.Path, nameEntry.Text)
		if err := fsops.Mkdir(target); err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		p.activeView().Reload()
	}, c.win)
	d.Resize(fyne.NewSize(560, 160))
	showDialog(d)
	// Focus + select the prefilled name so it's ready to type over
	// immediately — no click-to-focus needed first, matching every other
	// typing modal — Entry's select-all shortcut only takes effect once the
	// widget is actually rendered, which Show() just triggered.
	c.win.Canvas().Focus(nameEntry)
	nameEntry.TypedShortcut(&fyne.ShortcutSelectAll{})
}

// ── F8 / Shift+F8 Delete ─────────────────────────────────────────────────────

func (c *commander) doDeleteTrash()     { c.doDelete(false) }
func (c *commander) doDeletePermanent() { c.doDelete(true) }

func (c *commander) doDelete(permanent bool) {
	p := c.activePane()
	if c.blockIfArchive(p.activeView()) {
		return
	}
	paths := p.activeView().SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("nothing to delete")
		return
	}

	title := "Move to Trash"
	msg := fmt.Sprintf("Send %d item(s) to the trash?", len(paths))
	if permanent {
		title = "Delete Permanently"
		msg = fmt.Sprintf("PERMANENTLY delete %d item(s)? This cannot be undone.", len(paths))
	}
	showDialog(dialog.NewConfirm(title, msg, func(ok bool) {
		if !ok {
			return
		}
		go func() {
			err := fsops.Delete(paths, permanent)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				p.activeView().Reload()
			})
		}()
	}, c.win))
}

// ── shared progress + conflict machinery ────────────────────────────────────

// fsOpFunc matches fsops.Copy / fsops.Move's signature.
type fsOpFunc func(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error

// runFileOp runs op in the background with a progress dialog, reloading both
// the source pane and its opposite (the destination) when it finishes.
func (c *commander) runFileOp(verb string, sources []string, destDir string, op fsOpFunc, sourcePane *pane) {
	statusLbl := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	content := container.NewVBox(statusLbl, progressBar)
	prog := dialog.NewCustomWithoutButtons(verb, content, c.win)
	prog.Show()

	var applyToAll bool
	var appliedAction fsops.ConflictAction

	resolve := func(destPath string) (fsops.ConflictAction, string) {
		if applyToAll {
			return appliedAction, ""
		}
		resultCh := make(chan conflictResult, 1)
		fyne.Do(func() { showConflictDialog(c.win, destPath, resultCh) })
		res := <-resultCh
		if res.applyToAll {
			applyToAll = true
			appliedAction = res.action
		}
		return res.action, res.newName
	}

	progress := func(done, total int64, currentPath string) {
		fyne.Do(func() {
			statusLbl.SetText(filepath.Base(currentPath))
			if total > 0 {
				progressBar.SetValue(float64(done) / float64(total))
			}
		})
	}

	go func() {
		err := op(sources, destDir, progress, resolve)
		fyne.Do(func() {
			prog.Hide()
			if err != nil && err != fsops.ErrCancelled {
				dialog.ShowError(err, c.win)
			}
			sourcePane.activeView().Reload()
			c.inactivePaneOf(sourcePane).activeView().Reload()
		})
	}()
}

type conflictResult struct {
	action     fsops.ConflictAction
	newName    string
	applyToAll bool
}

// showConflictDialog must be called on the main goroutine (via fyne.Do); it
// sends exactly one result to resultCh once the user picks an action.
func showConflictDialog(win fyne.Window, destPath string, resultCh chan<- conflictResult) {
	base := filepath.Base(destPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	nameEntry := newDialogEntry()
	nameEntry.SetText(stem + " (copy)" + ext)
	applyAll := widget.NewCheck("Apply to all remaining conflicts", nil)

	var d *dialog.CustomDialog
	send := func(action fsops.ConflictAction, newName string) {
		resultCh <- conflictResult{action: action, newName: newName, applyToAll: applyAll.Checked}
		d.Hide()
	}

	buttons := container.NewGridWithColumns(4,
		widget.NewButton("Overwrite", func() { send(fsops.ConflictOverwrite, "") }),
		widget.NewButton("Skip", func() { send(fsops.ConflictSkip, "") }),
		widget.NewButton("Rename", func() { send(fsops.ConflictRename, nameEntry.Text) }),
		widget.NewButton("Cancel", func() { send(fsops.ConflictCancel, "") }),
	)

	content := container.NewVBox(
		widget.NewLabel(destPath+"\nalready exists."),
		nameEntry,
		applyAll,
		buttons,
	)
	d = dialog.NewCustomWithoutButtons("File Exists", content, win)
	// Escape reaches send() directly (same as the Cancel button) rather than
	// the generic d.Dismiss(): this dialog reports its result through
	// resultCh, not the dialog framework's own ok/cancel callback (it has
	// none — NewCustomWithoutButtons takes no callback), so a plain Dismiss
	// would just hide the dialog while resolve() stays blocked on resultCh
	// forever.
	showDialogWithDismiss(d, func() { send(fsops.ConflictCancel, "") })
	win.Canvas().Focus(nameEntry)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
