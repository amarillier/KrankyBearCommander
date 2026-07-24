// archive_ui.go — the optional 7-Zip binary path preference (File menu):
// SevenZipAvailable (internal/fsops) already finds 7z/7za/7zz on PATH, this
// is only for a binary installed somewhere PATH doesn't reach.
package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

const prefSevenZipPath = "sevenZipBinaryPath"

func (c *commander) loadSevenZipPath() {
	c.sevenZipPath = c.app.Preferences().String(prefSevenZipPath)
}

func (c *commander) saveSevenZipPath(path string) {
	c.sevenZipPath = path
	c.app.Preferences().SetString(prefSevenZipPath, path)
}

// showSevenZipSettings lets the user point at a 7z-capable binary that
// isn't on PATH — left blank, Compress falls back to a plain PATH lookup
// (see fsops.SevenZipAvailable), and if neither finds one, "Compress to
// .7z" simply doesn't appear in the context menu.
func (c *commander) showSevenZipSettings() {
	pathEntry := widget.NewEntry()
	pathEntry.SetText(c.sevenZipPath)
	pathEntry.SetPlaceHolder("Leave blank to auto-detect 7z/7za/7zz on PATH")

	browseBtn := widget.NewButton("Browse…", func() {
		d := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil || uc == nil {
				return
			}
			defer uc.Close()
			pathEntry.SetText(uc.URI().Path())
		}, c.win)
		if home, err := c.fs.HomeDir(); err == nil && home != "" {
			if uri := storage.NewFileURI(home); uri != nil {
				if lister, err := storage.ListerForURI(uri); err == nil {
					d.SetLocation(lister)
				}
			}
		}
		d.Show()
	})

	content := container.NewVBox(
		widget.NewLabel("Path to a 7z/7za/7zz binary, for Compress → To .7z:"),
		container.NewBorder(nil, nil, nil, browseBtn, pathEntry),
	)

	dialog.NewCustomConfirm("7-Zip Binary Path", "Save", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		c.saveSevenZipPath(strings.TrimSpace(pathEntry.Text))
	}, c.win).Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
