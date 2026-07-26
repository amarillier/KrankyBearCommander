// multirename_ui.go — the Multi-Rename Tool (Ctrl+M): TotalCmd-style batch
// rename with a pattern language, case conversion, and find/replace, with a
// live old→new preview before anything actually changes on disk. See
// internal/rename for the pattern logic itself (pure, unit-tested, no Fyne
// dependency) — this file is just the dialog and the actual on-disk apply.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/rename"
)

var multiRenameCaseOptions = []string{"No Change", "UPPERCASE", "lowercase", "Title Case", "Sentence case"}

func multiRenameCaseMode(label string) rename.CaseMode {
	switch label {
	case "UPPERCASE":
		return rename.CaseUpper
	case "lowercase":
		return rename.CaseLower
	case "Title Case":
		return rename.CaseTitle
	case "Sentence case":
		return rename.CaseSentence
	default:
		return rename.CaseNone
	}
}

// showMultiRenameTool is Ctrl+M and the File menu/F9 popup: batch-renames
// the active pane's selection (or just the cursor row).
func (c *commander) showMultiRenameTool() {
	c.showMultiRenameToolFor(c.activePane().activeView())
}

// showMultiRenameToolFor is the same, but for an explicit view — used by
// the right-click menu (see contextmenu_ui.go), since right-clicking a row
// doesn't make that row's pane the active one (same reasoning as
// copyToClipboard/pasteInto in clipboard_ui.go).
func (c *commander) showMultiRenameToolFor(view *fileListView) {
	if view == nil {
		return
	}
	if c.blockIfArchive(view) {
		return
	}
	names := view.SelectionOrCursorNames()
	if len(names) == 0 {
		c.showStatus("select file(s) to rename")
		return
	}
	dir := view.CurrentPath()

	patternEntry := widget.NewEntry()
	patternEntry.SetText("[N]")

	caseSelect := widget.NewSelect(multiRenameCaseOptions, nil)
	caseSelect.SetSelected("No Change")

	findEntry := widget.NewEntry()
	findEntry.SetPlaceHolder("Find (text or regex)")
	replaceEntry := widget.NewEntry()
	replaceEntry.SetPlaceHolder("Replace with")
	regexCheck := widget.NewCheck("Regex", nil)

	statusLbl := widget.NewLabel("")
	previews := make([]string, len(names))

	list := widget.NewList(
		func() int { return len(names) },
		func() fyne.CanvasObject {
			return container.NewGridWithColumns(2, widget.NewLabel(""), widget.NewLabel(""))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container).Objects
			row[0].(*widget.Label).SetText(names[id])
			row[1].(*widget.Label).SetText(previews[id])
		},
	)

	buildOpts := func() rename.Options {
		return rename.Options{
			Pattern:  patternEntry.Text,
			Case:     multiRenameCaseMode(caseSelect.Selected),
			Find:     findEntry.Text,
			Replace:  replaceEntry.Text,
			UseRegex: regexCheck.Checked,
		}
	}

	refreshPreview := func() {
		out, err := rename.PreviewBatch(buildOpts(), names)
		if err != nil {
			statusLbl.SetText("⚠ " + err.Error())
			copy(previews, names) // fall back to unchanged so the list stays populated
		} else {
			previews = out
			statusLbl.SetText(fmt.Sprintf("%d item(s)", len(names)))
		}
		list.Refresh()
	}
	refreshPreview()

	patternEntry.OnChanged = func(string) { refreshPreview() }
	caseSelect.OnChanged = func(string) { refreshPreview() }
	findEntry.OnChanged = func(string) { refreshPreview() }
	replaceEntry.OnChanged = func(string) { refreshPreview() }
	regexCheck.OnChanged = func(bool) { refreshPreview() }

	content := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Pattern:"),
			patternEntry,
			widget.NewLabel("[N] name  [N1-3] chars 1-3  [E] extension  [C]/[C:start]/[C:start,step]/[C:start,step,width] counter, e.g. [C:1,1,3] → 001, 002, …"),
			container.NewHBox(widget.NewLabel("Case:"), caseSelect),
			container.NewGridWithColumns(3, findEntry, replaceEntry, regexCheck),
			statusLbl,
			widget.NewSeparator(),
			container.NewGridWithColumns(2, widget.NewLabelWithStyle("Original", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), widget.NewLabelWithStyle("New name", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})),
		),
		nil, nil, nil,
		container.NewVScroll(list),
	)

	d := dialog.NewCustomConfirm("Multi-Rename Tool", "Rename", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		out, err := rename.PreviewBatch(buildOpts(), names)
		if err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		if err := applyMultiRename(dir, names, out); err != nil {
			dialog.ShowError(err, c.win)
		}
		view.Reload()
	}, c.win)
	d.Resize(multiRenameDialogSize(c.win))
	d.Show()
}

// multiRenameDialogSize scales the dialog to the main window so the
// Original/New name preview columns have room to actually show full names,
// instead of a fixed size that's fine for short names but clips longer ones.
func multiRenameDialogSize(win fyne.Window) fyne.Size {
	main := win.Canvas().Size()
	w := main.Width - 80
	if w < 640 {
		w = 640
	}
	h := main.Height - 120
	if h < 480 {
		h = 480
	}
	return fyne.NewSize(w, h)
}

// applyMultiRename validates the whole batch up front (no duplicate result
// names, none colliding with an unrelated existing file), then renames via
// unique temporary names first and only then to their final names — so
// same-batch collisions/permutations (e.g. swapping two files' names, or
// shifting a whole sequence) can never clobber each other, without needing
// a per-item conflict dialog for what's meant to be one bulk operation.
func applyMultiRename(dir string, oldNames, newNames []string) error {
	oldSet := make(map[string]bool, len(oldNames))
	for _, n := range oldNames {
		oldSet[n] = true
	}

	newSet := make(map[string]bool, len(newNames))
	anyChanged := false
	for i, newName := range newNames {
		if newName == oldNames[i] {
			continue
		}
		anyChanged = true
		if newSet[newName] {
			return fmt.Errorf("%q would result from more than one renamed item", newName)
		}
		newSet[newName] = true
	}
	if !anyChanged {
		return nil
	}

	for _, newName := range newNames {
		if oldSet[newName] {
			continue // part of this batch — safe via the temp-name pass below
		}
		if _, err := os.Lstat(filepath.Join(dir, newName)); err == nil {
			return fmt.Errorf("%q already exists", newName)
		}
	}

	temps := make([]string, len(oldNames))
	for i, old := range oldNames {
		if old == newNames[i] {
			continue
		}
		tmp := fmt.Sprintf(".kbc-rename-tmp-%d-%d", os.Getpid(), i)
		if err := fsops.Rename(filepath.Join(dir, old), filepath.Join(dir, tmp)); err != nil {
			return fmt.Errorf("renaming %q: %w", old, err)
		}
		temps[i] = tmp
	}
	for i, newName := range newNames {
		if oldNames[i] == newName {
			continue
		}
		if err := fsops.Rename(filepath.Join(dir, temps[i]), filepath.Join(dir, newName)); err != nil {
			return fmt.Errorf("renaming to %q: %w", newName, err)
		}
	}
	return nil
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
