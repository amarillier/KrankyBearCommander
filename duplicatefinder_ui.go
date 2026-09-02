// duplicatefinder_ui.go — Find Duplicate Files (File menu / F9 popup /
// pane toolbar copy-icon button / Ctrl+D): recursively scans the active pane's
// active tab's current directory for files with byte-identical content
// (see internal/fsops.FindDuplicates — content-based, unlike the
// right-click "Duplicate" which just clones a file), then lets you review
// every duplicate with a per-file Keep/Delete choice before confirming.
// Recursion depth defaults to unlimited but can be capped (blank = same as
// today) via a prompt before the scan starts, in case a huge tree makes
// the overhead worth bounding — same idea as search_ui.go's own depth
// limit, just a free-form number here rather than a preset list. Local
// directories only, same scope as Verify After Copy (see fileops_ui.go's
// crossFSCopyOp) — there's no proven need yet to scan through a remote
// connection.
package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/vfs/listboxfs"
	"commander/internal/vfs/zipfs"
)

// prefDuplicatesMaxDepth persists the last-entered max-depth text (blank =
// unlimited) across launches, same convention as search_ui.go's own
// prefSearchDepth.
const prefDuplicatesMaxDepth = "duplicatesMaxDepth"

func (c *commander) doFindDuplicates() {
	p := c.activePane()
	view := p.activeView()
	if view == nil {
		return
	}
	if _, ok := view.fs.(*zipfs.FS); ok {
		c.showStatus("duplicate finder isn't available inside archives")
		return
	}
	if _, ok := view.fs.(*listboxfs.FS); ok {
		c.showStatus("duplicate finder isn't available in a listbox view — press Home first")
		return
	}
	if _, ok := view.fs.(remoteConnFS); ok {
		dialog.ShowInformation("Not Supported Yet", "Find Duplicate Files isn't available yet for a remote connection — local directories only.", c.win)
		return
	}
	root := view.CurrentPath()

	prefs := c.app.Preferences()
	depthEntry := newDialogEntry()
	depthEntry.SetPlaceHolder("Unlimited")
	depthEntry.SetText(prefs.String(prefDuplicatesMaxDepth))

	d := dialog.NewForm("Find Duplicate Files", "Scan", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Max subdirectory depth (blank = unlimited)", depthEntry)},
		func(ok bool) {
			if !ok {
				return
			}
			text := strings.TrimSpace(depthEntry.Text)
			prefs.SetString(prefDuplicatesMaxDepth, text)
			// A blank or unparseable/negative entry means unlimited (-1),
			// same as leaving it alone — a typo here shouldn't block the
			// scan with a validation error over a single optional field.
			maxDepth := -1
			if n, err := strconv.Atoi(text); err == nil && n >= 0 {
				maxDepth = n
			}
			c.runFindDuplicatesScan(root, view, maxDepth)
		}, c.win)
	// NewForm sizes to its content's natural MinSize, which left the Entry
	// too narrow to show its own "Unlimited" placeholder in full (it fell
	// back to a functional but odd-looking internal scroll) — widen just
	// the dialog, not the Entry itself, since Form's layout stretches the
	// input column to fill whatever width the dialog is given.
	size := d.MinSize()
	size.Width = 460
	d.Resize(size)
	showDialog(d)
}

// runFindDuplicatesScan shows the scan progress dialog and, on completion,
// the review dialog — split out from doFindDuplicates so the max-depth
// prompt above it can reuse the same flow.
func (c *commander) runFindDuplicatesScan(root string, view *fileListView, maxDepth int) {
	statusLbl := widget.NewLabel("Scanning…")
	progressBar := widget.NewProgressBarInfinite()
	var cancelled bool
	cancelBtn := widget.NewButton("Cancel", func() { cancelled = true })
	content := container.NewVBox(statusLbl, progressBar, cancelBtn)
	prog := dialog.NewCustomWithoutButtons("Finding Duplicate Files", content, c.win)
	// Escape matches the Cancel button, same as Calculate Folder Sizes'
	// identical progress-dialog pattern (foldersize_ui.go).
	showDialogWithDismiss(prog, func() { cancelled = true })

	go func() {
		groups, err := fsops.FindDuplicates(root, c.showHiddenFiles, maxDepth, func(phase, currentPath string) bool {
			fyne.Do(func() { statusLbl.SetText(phase) })
			return !cancelled
		})
		fyne.Do(func() {
			prog.Hide()
			if err != nil {
				if err != fsops.ErrCancelled {
					dialog.ShowError(err, c.win)
				}
				return
			}
			if len(groups) == 0 {
				c.showStatus("no duplicate files found")
				return
			}
			c.showDuplicatesDialog(root, groups, view)
		})
	}()
}

const (
	dupActionKeep   = "Keep"
	dupActionDelete = "Delete"
)

// dupRow is one file within one duplicate group — one row per file (not per
// group), same flattened shape comparesync_ui.go's compareRow uses for its
// own per-item Select.
type dupRow struct {
	groupIdx int
	path     string
	size     int64
	action   string
}

// buildDupRows flattens every group's files into rows, defaulting the
// first file in each group to Keep and every other file to Delete — a
// sensible default (confirming with no changes already reclaims the
// group's duplicate space) that the user can freely override per row,
// same spirit as Compare/Synchronize's own per-row defaults.
func buildDupRows(groups []fsops.DuplicateGroup) []dupRow {
	var rows []dupRow
	for gi, g := range groups {
		for i, path := range g.Paths {
			action := dupActionDelete
			if i == 0 {
				action = dupActionKeep
			}
			rows = append(rows, dupRow{groupIdx: gi, path: path, size: g.Size, action: action})
		}
	}
	return rows
}

// showDuplicatesDialog reviews every duplicate found and, on confirm,
// deletes whichever rows are set to Delete — mirrors comparesync_ui.go's
// showCompareDialog/compareRow wiring almost exactly.
func (c *commander) showDuplicatesDialog(root string, groups []fsops.DuplicateGroup, view *fileListView) {
	rows := buildDupRows(groups)

	// Group/Size/Action sit at their natural (narrow) content width via
	// Border's left/right edges; Path — the field that actually needs
	// room, especially for the long descriptive filenames this feature
	// targets (trail-camera/photo dedup) — gets all remaining space and
	// ellipsizes instead of overflowing into the Size column next to it
	// (the equal-width grid this replaced let a long name visually
	// overlap Size, making it unreadable).
	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject {
			groupLbl := widget.NewLabel("")
			pathLbl := widget.NewLabel("")
			pathLbl.Truncation = fyne.TextTruncateEllipsis
			sizeLbl := widget.NewLabel("")
			sizeLbl.Alignment = fyne.TextAlignTrailing
			sel := widget.NewSelect([]string{dupActionKeep, dupActionDelete}, nil)
			trailing := container.NewHBox(sizeLbl, sel)
			return container.NewBorder(nil, nil, groupLbl, trailing, pathLbl)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			pathLbl := row.Objects[0].(*widget.Label)
			groupLbl := row.Objects[1].(*widget.Label)
			trailing := row.Objects[2].(*fyne.Container)
			sizeLbl := trailing.Objects[0].(*widget.Label)
			sel := trailing.Objects[1].(*widget.Select)

			r := &rows[id]
			rel, err := filepath.Rel(root, r.path)
			if err != nil {
				rel = r.path
			}
			groupLbl.SetText(fmt.Sprintf("Group %d", r.groupIdx+1))
			pathLbl.SetText(rel)
			sizeLbl.SetText(humanSize(r.size))
			sel.OnChanged = nil // avoid firing while set up for a (possibly different) recycled row
			sel.SetSelected(r.action)
			sel.OnChanged = func(chosen string) { r.action = chosen }
		},
	)

	header := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("Group", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewLabelWithStyle("Size", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true}),
			widget.NewLabelWithStyle("Action", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		),
		widget.NewLabelWithStyle("Path", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	var totalWaste int64
	for _, g := range groups {
		totalWaste += g.Size * int64(len(g.Paths)-1)
	}

	feedBtn := widget.NewButton("Feed to Listbox", func() {
		names := duplicateListboxNames(root, rows)
		if !view.enterListbox(fmt.Sprintf("Duplicates: %s", root), names) {
			c.showStatus("tab is locked")
		}
	})

	content := container.NewBorder(
		container.NewVBox(
			widget.NewLabel(fmt.Sprintf("%d duplicate file(s) in %d group(s) — %s reclaimable", len(rows), len(groups), humanSize(totalWaste))),
			header,
			container.NewHBox(feedBtn),
		),
		nil, nil, nil,
		list,
	)

	d := dialog.NewCustomConfirm("Find Duplicate Files", "Delete Selected", "Close", content, func(ok bool) {
		if !ok {
			return
		}
		var toDelete []string
		for _, r := range rows {
			if r.action == dupActionDelete {
				toDelete = append(toDelete, r.path)
			}
		}
		if len(toDelete) == 0 {
			c.showStatus("nothing marked for deletion")
			return
		}
		c.confirmDeleteDuplicates(toDelete, view)
	}, c.win)
	d.Resize(multiRenameDialogSize(c.win))
	showDialog(d)
}

// duplicateListboxNames assigns each row's file a display name for
// listboxfs, same disambiguation scheme as search_ui.go's listboxNames: the
// plain basename normally, falling back to the path relative to root (then
// the full path) for any name that collides with another row's basename.
func duplicateListboxNames(root string, rows []dupRow) map[string]string {
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[filepath.Base(r.path)]++
	}
	names := make(map[string]string, len(rows))
	for _, r := range rows {
		name := filepath.Base(r.path)
		if counts[name] > 1 {
			if rel, err := filepath.Rel(root, r.path); err == nil {
				name = rel
			} else {
				name = r.path
			}
		}
		if _, exists := names[name]; exists {
			name = r.path
		}
		names[name] = r.path
	}
	return names
}

// confirmDeleteDuplicates sends every one of paths to the trash (not
// permanent — same default deleteCompareSyncPaths' local branch uses).
func (c *commander) confirmDeleteDuplicates(paths []string, view *fileListView) {
	showDialog(dialog.NewConfirm("Delete Duplicate Files", fmt.Sprintf("Send %d duplicate file(s) to the trash?", len(paths)), func(ok bool) {
		if !ok {
			return
		}
		go func() {
			err := fsops.Delete(paths, false)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				view.Reload()
			})
		}()
	}, c.win))
}
