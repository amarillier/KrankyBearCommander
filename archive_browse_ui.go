// archive_browse_ui.go — the Fyne-facing half of browsing into .zip
// archives (see internal/vfs/zipfs for the read-only virtual filesystem
// itself, and filelist.go's enterZip/adjustFSForTarget for the pane-level
// navigation in and out of one). This file covers what happens to an
// individual archived file: F3 View and Enter/double-click both need a
// real temp copy on disk since nothing actually exists at a presented
// archive path, and F3 View on a .zip file itself (before browsing into
// it) offers a lightweight "pick one member to preview" list instead of
// requiring full pane-browsing just to glance at one file.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/vfs/zipfs"
)

// extractMemberToTemp copies zfs's member at presentedPath to a fresh temp
// file preserving its base name (so OS default-app association still works
// via extension), returning the temp file's real path. The temp file is
// deliberately left behind for the OS to eventually clean up rather than
// deleted immediately — whatever opens it (the viewer, or the OS's default
// application) may still be reading it asynchronously.
func extractMemberToTemp(zfs *zipfs.FS, presentedPath, name string) (string, error) {
	rc, err := zfs.Open(presentedPath)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	dir, err := os.MkdirTemp("", "krankybear-zip-*")
	if err != nil {
		return "", err
	}
	tempPath := filepath.Join(dir, name)
	out, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return "", err
	}
	return tempPath, out.Close()
}

// openArchivedMember is Enter/double-click on a file inside an open
// archive (filelist.go's activate, via onOpenArchivedMember) — extracts it
// to a temp copy, then opens that with the OS's default application, same
// as double-clicking a real file.
func (c *commander) openArchivedMember(zfs *zipfs.FS, name, presentedPath string) {
	tempPath, err := extractMemberToTemp(zfs, presentedPath, name)
	if err != nil {
		c.showStatus("cannot extract " + name + ": " + err.Error())
		return
	}
	openWithOS(tempPath)
}

// viewArchivedMember is F3 View on a file inside an open archive —
// extracts it to a temp copy, then opens it in the normal read-only viewer.
func (c *commander) viewArchivedMember(zfs *zipfs.FS, name, presentedPath string) {
	tempPath, err := extractMemberToTemp(zfs, presentedPath, name)
	if err != nil {
		c.showStatus("cannot extract " + name + ": " + err.Error())
		return
	}
	showViewer(c.app, tempPath)
}

// showZipPreviewPicker is F3 View on a .zip file while still browsing the
// real filesystem (i.e. before browsing into it) — a lightweight
// alternative to full pane-browsing for "I just want to glance at one file
// in this archive." Picking a member extracts and views just that one.
func (c *commander) showZipPreviewPicker(zipPath string) {
	zfs, err := zipfs.Open(zipPath)
	if err != nil {
		c.showStatus("cannot open archive: " + err.Error())
		return
	}
	files := zfs.AllFiles()

	var d dialog.Dialog
	closed := false
	closeAndRelease := func() {
		if !closed {
			closed = true
			zfs.Close()
		}
	}

	list := widget.NewList(
		func() int { return len(files) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(strings.TrimPrefix(files[id], zipPath+"/"))
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(files) {
			return
		}
		presented := files[id]
		c.viewArchivedMember(zfs, filepath.Base(presented), presented)
		d.Hide()
	}

	content := container.NewBorder(
		widget.NewLabel(fmt.Sprintf("%d file(s) in %s — pick one to view:", len(files), filepath.Base(zipPath))),
		nil, nil, nil,
		container.NewVScroll(list),
	)

	d = dialog.NewCustom("Preview Archive", "Close", content, c.win)
	d.Resize(fyne.NewSize(480, 420))
	d.SetOnClosed(closeAndRelease)
	showDialog(d)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
