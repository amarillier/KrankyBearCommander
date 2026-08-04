// attributes_ui.go — the "Change Attributes…" batch dialog: Read-only/
// Hidden/Archive/System (platform-conditional — see internal/fsops's
// SetAttributes doc comment) and the modified timestamp, optionally
// recursing into subdirectories. Local filesystem only: an archive,
// listbox, or remote connection's presented paths aren't real local paths
// for os.Chmod/os.Chtimes to find, and Windows-style attributes don't map
// onto a remote connection cleanly anyway.
package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
)

var attrChangeOptions = []string{"Unchanged", "Set", "Clear"}

func attrChangeFromLabel(label string) fsops.AttrChange {
	switch label {
	case "Set":
		return fsops.AttrSet
	case "Clear":
		return fsops.AttrClear
	default:
		return fsops.AttrUnchanged
	}
}

// showChangeAttributes is Ctrl+M's Change Attributes counterpart: batch-
// edits the active pane's selection (or just the cursor row — the same
// "selection if any, else cursor" rule F5/F6/F8/Multi-Rename all use).
func (c *commander) showChangeAttributes() {
	c.showChangeAttributesFor(c.activePane().activeView())
}

// showChangeAttributesFor is the same, but for an explicit view — used by
// the right-click menu (see contextmenu_ui.go), since right-clicking a row
// doesn't make that row's pane the active one.
func (c *commander) showChangeAttributesFor(view *fileListView) {
	if view == nil {
		return
	}
	if c.blockIfArchive(view) || c.blockIfListbox(view) || c.blockIfRemote(view) {
		return
	}
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("select file(s) to change attributes for")
		return
	}

	recurseCheck := widget.NewCheck("Recurse subdirectories", nil)

	rows := []fyne.CanvasObject{
		recurseCheck,
		widget.NewSeparator(),
		widget.NewLabel("Change attributes:"),
	}

	// Windows keeps the single simplified Read-only toggle (its own closest
	// concept — there's no POSIX permission model to expose there). macOS/
	// Linux get the full Owner/Group/Other × Read/Write/Execute grid
	// instead — see fsops.Attributes' doc comment for why permission-grid
	// changes, unlike ReadOnly, aren't exempted from directories.
	newAttrSelect := func() *widget.Select {
		s := widget.NewSelect(attrChangeOptions, nil)
		s.SetSelected("Unchanged")
		return s
	}
	var readOnlySelect *widget.Select
	var ownerReadSelect, ownerWriteSelect, ownerExecSelect *widget.Select
	var groupReadSelect, groupWriteSelect, groupExecSelect *widget.Select
	var otherReadSelect, otherWriteSelect, otherExecSelect *widget.Select
	if runtime.GOOS == "windows" {
		readOnlySelect = newAttrSelect()
		rows = append(rows, container.NewGridWithColumns(2, widget.NewLabel("Read-only:"), readOnlySelect))
	} else {
		ownerReadSelect, ownerWriteSelect, ownerExecSelect = newAttrSelect(), newAttrSelect(), newAttrSelect()
		groupReadSelect, groupWriteSelect, groupExecSelect = newAttrSelect(), newAttrSelect(), newAttrSelect()
		otherReadSelect, otherWriteSelect, otherExecSelect = newAttrSelect(), newAttrSelect(), newAttrSelect()
		rows = append(rows,
			widget.NewLabel("Permissions:"),
			container.NewGridWithColumns(4, widget.NewLabel(""), widget.NewLabel("Read"), widget.NewLabel("Write"), widget.NewLabel("Execute")),
			container.NewGridWithColumns(4, widget.NewLabel("Owner"), ownerReadSelect, ownerWriteSelect, ownerExecSelect),
			container.NewGridWithColumns(4, widget.NewLabel("Group"), groupReadSelect, groupWriteSelect, groupExecSelect),
			container.NewGridWithColumns(4, widget.NewLabel("Other"), otherReadSelect, otherWriteSelect, otherExecSelect),
		)
	}

	// Hidden has a real attribute bit on Windows and macOS; on Linux
	// "hidden" is a leading-dot filename convention, not a settable
	// attribute, so the checkbox is simply not offered there rather than
	// silently doing nothing.
	var hiddenSelect *widget.Select
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		hiddenSelect = newAttrSelect()
		rows = append(rows, container.NewGridWithColumns(2, widget.NewLabel("Hidden:"), hiddenSelect))
	}
	// Archive/System have no equivalent outside Windows at all.
	var archiveSelect, systemSelect *widget.Select
	if runtime.GOOS == "windows" {
		archiveSelect = newAttrSelect()
		systemSelect = newAttrSelect()
		rows = append(rows,
			container.NewGridWithColumns(2, widget.NewLabel("Archive:"), archiveSelect),
			container.NewGridWithColumns(2, widget.NewLabel("System:"), systemSelect),
		)
	}

	setTimeCheck := widget.NewCheck("Change modified date/time:", nil)
	dateEntry := newDialogEntry()
	dateEntry.SetPlaceHolder("YYYY-MM-DD")
	timeEntry := newDialogEntry()
	timeEntry.SetPlaceHolder("HH:MM:SS")
	fillNow := func() {
		now := time.Now()
		dateEntry.SetText(now.Format("2006-01-02"))
		timeEntry.SetText(now.Format("15:04:05"))
	}
	fillNow() // prefilled so checking the box and clicking Change with no
	// further input just sets everything to "now" — Now below is only
	// needed to refresh it after leaving the dialog open a while.
	nowBtn := widget.NewButton("Now", fillNow)
	dateTimeRow := container.NewBorder(nil, nil, nil, nowBtn, container.NewGridWithColumns(2, dateEntry, timeEntry))
	rows = append(rows, widget.NewSeparator(), setTimeCheck, dateTimeRow)

	content := container.NewVBox(rows...)

	d := dialog.NewCustomConfirm(fmt.Sprintf("Change Attributes — %d item(s)", len(paths)), "Change", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		var attrs fsops.Attributes
		if readOnlySelect != nil {
			attrs.ReadOnly = attrChangeFromLabel(readOnlySelect.Selected)
		}
		if ownerReadSelect != nil {
			attrs.OwnerRead = attrChangeFromLabel(ownerReadSelect.Selected)
			attrs.OwnerWrite = attrChangeFromLabel(ownerWriteSelect.Selected)
			attrs.OwnerExecute = attrChangeFromLabel(ownerExecSelect.Selected)
			attrs.GroupRead = attrChangeFromLabel(groupReadSelect.Selected)
			attrs.GroupWrite = attrChangeFromLabel(groupWriteSelect.Selected)
			attrs.GroupExecute = attrChangeFromLabel(groupExecSelect.Selected)
			attrs.OtherRead = attrChangeFromLabel(otherReadSelect.Selected)
			attrs.OtherWrite = attrChangeFromLabel(otherWriteSelect.Selected)
			attrs.OtherExecute = attrChangeFromLabel(otherExecSelect.Selected)
		}
		if hiddenSelect != nil {
			attrs.Hidden = attrChangeFromLabel(hiddenSelect.Selected)
		}
		if archiveSelect != nil {
			attrs.Archive = attrChangeFromLabel(archiveSelect.Selected)
		}
		if systemSelect != nil {
			attrs.System = attrChangeFromLabel(systemSelect.Selected)
		}
		if setTimeCheck.Checked {
			t, err := time.ParseInLocation("2006-01-02 15:04:05",
				strings.TrimSpace(dateEntry.Text)+" "+strings.TrimSpace(timeEntry.Text), time.Local)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid date/time: %w", err), c.win)
				return
			}
			attrs.SetTime = true
			attrs.ModTime = t
		}
		recurse := recurseCheck.Checked
		go func() {
			err := fsops.SetAttributes(paths, recurse, attrs)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				view.Reload()
			})
		}()
	}, c.win)
	d.Resize(fyne.NewSize(420, 480))
	showDialog(d)
}
