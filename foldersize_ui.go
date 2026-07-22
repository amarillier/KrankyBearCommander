// foldersize_ui.go — "Calculate Folder Sizes" (File menu / F9 popup): walks
// every directory in the active pane's active tab (du -s-style, via
// internal/fsops.DirSize) and fills in real sizes where the Size column
// otherwise just shows "<DIR>", including the ".." row, which shows the
// current directory's own total. Deliberately one simple action over the
// whole listing — no separate "selected only" vs "all" modes.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
)

func (c *commander) doCalculateFolderSizes() {
	v := c.activePane().activeView()
	if v == nil {
		return
	}
	names := v.DirEntryNames()
	currentPath := v.CurrentPath()
	if len(names) == 0 {
		c.showStatus("no subdirectories to calculate here")
		return
	}

	statusLbl := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	var cancelled bool
	cancelBtn := widget.NewButton("Cancel", func() { cancelled = true })
	content := container.NewVBox(statusLbl, progressBar, cancelBtn)
	prog := dialog.NewCustomWithoutButtons("Calculating Folder Sizes", content, c.win)
	prog.Show()

	go func() {
		total := len(names) + 1 // +1 for the current directory's own total
		var done int
		report := func(label string) {
			done++
			fyne.Do(func() {
				statusLbl.SetText(label)
				progressBar.SetValue(float64(done) / float64(total))
			})
		}

		for _, name := range names {
			if cancelled {
				break
			}
			if size, err := fsops.DirSize(v.FullPath(name)); err == nil {
				fyne.Do(func() { v.SetComputedSize(name, size) })
			}
			report(name)
		}

		if !cancelled {
			if size, err := fsops.DirSize(currentPath); err == nil {
				fyne.Do(func() { v.SetComputedParentSize(size) })
			}
			report("(current directory total)")
		}

		fyne.Do(func() { prog.Hide() })
	}()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
