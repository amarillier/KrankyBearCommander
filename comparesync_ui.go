// comparesync_ui.go — Compare/Synchronize Directories (File menu / F9
// popup, or a per-pane toolbar button): a single-level (this directory
// only, not its subfolders) comparison of the two panes' active tabs,
// showing files that differ or are missing on one side, with a per-row
// Skip/Copy/Delete choice. Copy reuses the exact same machinery F5 Copy
// already does (crossFSCopyOp/runFileOp, fileops_ui.go); Delete mirrors
// doDelete's own remoteConnFS check (fileops_ui.go) — both work across any
// combination of backends (local/SFTP/SMB/FileAgent) exactly like F5/F8 do.
package main

import (
	"fmt"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/vfs"
)

// compareModTimeTolerance absorbs whole-second-only mtime precision on
// FileAgent/zipfs (see their Entry construction) — without this, a
// byte-identical file re-fetched through either backend could show a
// spurious "different" purely from sub-second truncation.
const compareModTimeTolerance = 2 * time.Second

const (
	compareActionSkip        = "Skip"
	compareActionCopyRight   = "Copy → Right"
	compareActionCopyLeft    = "Copy ← Left"
	compareActionDeleteLeft  = "Delete Left"
	compareActionDeleteRight = "Delete Right"
)

// comparePrimary marks which pane (if any) the user has told this
// comparison to treat as the "source of truth" — set by clicking a
// specific pane's own toolbar button (as opposed to the neutral File
// menu/F9 popup entry, which has no opinion on which side is right).
// It only changes each row's DEFAULT action, never which options are
// offered — the user can always override any row's choice regardless.
type comparePrimary int

const (
	comparePrimaryNone comparePrimary = iota
	comparePrimaryLeft
	comparePrimaryRight
)

// compareRow is one differing/missing file — nil entries mean "doesn't
// exist on that side."
type compareRow struct {
	name    string
	left    *vfs.Entry
	right   *vfs.Entry
	options []string
	action  string
}

// showCompareSync builds the diff between the two panes' active tabs and,
// if there's anything to show, opens the review dialog. primary is
// comparePrimaryNone from the File menu/F9 popup, or whichever pane's own
// toolbar button was clicked — see comparePrimary's doc comment.
func (c *commander) showCompareSync(primary comparePrimary) {
	leftView, rightView := c.left.activeView(), c.right.activeView()
	if leftView == nil || rightView == nil {
		return
	}
	rows := buildCompareRows(leftView.entries, rightView.entries, primary)
	if len(rows) == 0 {
		c.showStatus("Nothing to compare — these two directories already match")
		return
	}
	c.showCompareDialog(rows, leftView, rightView)
}

// buildCompareRows matches left/right by name (files only — directories
// aren't compared in this single-level version) and classifies each name
// into "only left," "only right," or "both, but differs." A name present
// on both sides with equal size and a within-tolerance mtime is left out
// entirely — this dialog lists differences, not everything.
//
// Default action per row depends on primary: with no primary chosen, a
// missing-on-one-side file defaults to copying across (bring it to the
// side that doesn't have it) and a differing file defaults to Skip (which
// side is "right" is ambiguous). With a primary chosen, defaults instead
// aim to make the OTHER side match it: the primary's own file always wins
// (copies across / overwrites), and a file that only exists on the
// non-primary side — something the primary doesn't have — defaults to
// being deleted from the non-primary side rather than pulled over, since
// the point of choosing a primary is mirroring it, not merging both sides'
// extras into it.
func buildCompareRows(left, right []vfs.Entry, primary comparePrimary) []compareRow {
	leftByName := make(map[string]vfs.Entry, len(left))
	for _, e := range left {
		if !e.IsDir {
			leftByName[e.Name] = e
		}
	}
	rightByName := make(map[string]vfs.Entry, len(right))
	for _, e := range right {
		if !e.IsDir {
			rightByName[e.Name] = e
		}
	}

	names := make(map[string]bool, len(leftByName)+len(rightByName))
	for n := range leftByName {
		names[n] = true
	}
	for n := range rightByName {
		names[n] = true
	}

	var rows []compareRow
	for name := range names {
		l, hasLeft := leftByName[name]
		r, hasRight := rightByName[name]
		switch {
		case hasLeft && hasRight:
			if entriesEqual(l, r) {
				continue
			}
			action := compareActionSkip
			switch primary {
			case comparePrimaryLeft:
				action = compareActionCopyRight
			case comparePrimaryRight:
				action = compareActionCopyLeft
			}
			rows = append(rows, compareRow{
				name:    name,
				left:    &l,
				right:   &r,
				options: []string{compareActionSkip, compareActionCopyRight, compareActionCopyLeft},
				action:  action,
			})
		case hasLeft:
			action := compareActionCopyRight
			if primary == comparePrimaryRight {
				action = compareActionDeleteLeft
			}
			rows = append(rows, compareRow{
				name:    name,
				left:    &l,
				options: []string{compareActionSkip, compareActionCopyRight, compareActionDeleteLeft},
				action:  action,
			})
		default:
			action := compareActionCopyLeft
			if primary == comparePrimaryLeft {
				action = compareActionDeleteRight
			}
			rows = append(rows, compareRow{
				name:    name,
				right:   &r,
				options: []string{compareActionSkip, compareActionCopyLeft, compareActionDeleteRight},
				action:  action,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

func entriesEqual(a, b vfs.Entry) bool {
	if a.Size != b.Size {
		return false
	}
	diff := a.ModTime.Sub(b.ModTime)
	if diff < 0 {
		diff = -diff
	}
	return diff <= compareModTimeTolerance
}

// compareSideText is what a row's Left/Right column shows — size + mtime,
// or an em dash when the file doesn't exist on that side.
func compareSideText(e *vfs.Entry) string {
	if e == nil {
		return "—"
	}
	return fmt.Sprintf("%s  %s", humanSize(e.Size), e.ModTime.Format("2006-01-02 15:04"))
}

// showCompareDialog reviews rows and, on confirm, acts on whichever ones
// are no longer set to Skip.
func (c *commander) showCompareDialog(rows []compareRow, leftView, rightView *fileListView) {
	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject {
			return container.NewGridWithColumns(4, widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel(""), widget.NewSelect(nil, nil))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container).Objects
			r := &rows[id]
			row[0].(*widget.Label).SetText(r.name)
			row[1].(*widget.Label).SetText(compareSideText(r.left))
			row[2].(*widget.Label).SetText(compareSideText(r.right))
			sel := row[3].(*widget.Select)
			sel.Options = r.options
			sel.OnChanged = nil // avoid firing while we set it up for a (possibly different) row being recycled
			sel.SetSelected(r.action)
			sel.OnChanged = func(chosen string) { r.action = chosen }
		},
	)

	header := container.NewGridWithColumns(4,
		widget.NewLabelWithStyle("Name", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Left", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Right", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Action", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	content := container.NewBorder(
		container.NewVBox(widget.NewLabel(fmt.Sprintf("%d item(s) differ or are missing on one side:", len(rows))), header),
		nil, nil, nil,
		list,
	)

	d := dialog.NewCustomConfirm("Compare/Synchronize Directories", "Synchronize", "Close", content, func(ok bool) {
		if !ok {
			return
		}
		c.executeCompareSync(rows, leftView, rightView)
	}, c.win)
	d.Resize(multiRenameDialogSize(c.win))
	showDialog(d)
}

// executeCompareSync groups rows by action and runs each group through the
// same machinery the F5 Copy/F8 Delete commands already use — copies via
// crossFSCopyOp+runFileOp (fileops_ui.go), deletes via
// deleteCompareSyncPaths below — once per group that actually has anything
// in it.
func (c *commander) executeCompareSync(rows []compareRow, leftView, rightView *fileListView) {
	var toRight, toLeft, deleteLeft, deleteRight []string
	for _, r := range rows {
		switch r.action {
		case compareActionCopyRight:
			toRight = append(toRight, leftView.FullPath(r.name))
		case compareActionCopyLeft:
			toLeft = append(toLeft, rightView.FullPath(r.name))
		case compareActionDeleteLeft:
			deleteLeft = append(deleteLeft, leftView.FullPath(r.name))
		case compareActionDeleteRight:
			deleteRight = append(deleteRight, rightView.FullPath(r.name))
		}
	}
	if len(toRight) == 0 && len(toLeft) == 0 && len(deleteLeft) == 0 && len(deleteRight) == 0 {
		c.showStatus("Nothing selected to synchronize")
		return
	}
	if len(toRight) > 0 {
		verb, op := crossFSCopyOp(leftView, rightView)
		c.runFileOp(verb+"ing", toRight, rightView.CurrentPath(), op, c.left)
	}
	if len(toLeft) > 0 {
		verb, op := crossFSCopyOp(rightView, leftView)
		c.runFileOp(verb+"ing", toLeft, leftView.CurrentPath(), op, c.right)
	}
	if len(deleteLeft) > 0 {
		c.deleteCompareSyncPaths(deleteLeft, leftView)
	}
	if len(deleteRight) > 0 {
		c.deleteCompareSyncPaths(deleteRight, rightView)
	}
}

// deleteCompareSyncPaths removes paths from view's own backend — a real
// permanent delete on a connection tab (no trash on the other end),
// otherwise the normal trash. Mirrors doDelete's own remoteConnFS check
// exactly (fileops_ui.go), with its own confirmation first, since delete
// is the one destructive action this dialog offers and Allan explicitly
// wanted it available "user beware" rather than left out entirely.
func (c *commander) deleteCompareSyncPaths(paths []string, view *fileListView) {
	remoteFS, remote := view.fs.(remoteConnFS)
	title := "Delete to Synchronize"
	msg := fmt.Sprintf("Delete %d item(s) to synchronize?", len(paths))
	if remote {
		msg = fmt.Sprintf("PERMANENTLY delete %d item(s) to synchronize? This cannot be undone.", len(paths))
	}
	showDialog(dialog.NewConfirm(title, msg, func(ok bool) {
		if !ok {
			return
		}
		go func() {
			var err error
			if remote {
				for _, path := range paths {
					if rmErr := remoteFS.Remove(path); rmErr != nil {
						err = rmErr
						break
					}
				}
			} else {
				err = fsops.Delete(paths, false) // trash, not permanent — Compare/Sync has no Shift+F8-style explicit-permanent gesture, so default to the safer local path
			}
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				view.Reload()
			})
		}()
	}, c.win))
}
