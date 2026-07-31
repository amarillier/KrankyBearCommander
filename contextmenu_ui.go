// contextmenu_ui.go — the right-click context menu for a file/directory row:
// Open, Open With (configured external editors), Duplicate, Move to Trash,
// Copy Name/Path, Compress, Create Symbolic Link, Reveal in File Manager,
// Reveal in Opposite Pane, and (for directories) Add to Favorites. Wired
// from fileListView.onContextMenu (filelist.go) via pane.onContextMenu
// (paneview.go) — see keyTable.TappedSecondary's doc comment for how the
// Table view resolves a right-click to a row.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/launch"
	"commander/internal/panelstate"
	"commander/internal/vfs"
	"commander/internal/vfs/listboxfs"
	"commander/internal/vfs/zipfs"
)

// showRowContextMenu builds and shows the popup for name (already resolved
// to a real row — offerContextMenu excludes "" and "..").
func (c *commander) showRowContextMenu(p *pane, view *fileListView, name string, pos fyne.Position) {
	entry, fullPath, ok := view.entryAndPath(name)
	if !ok {
		return
	}

	if zfs, insideArchive := view.fs.(*zipfs.FS); insideArchive {
		c.showArchivedRowContextMenu(zfs, view, entry, fullPath, p, pos)
		return
	}

	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Open", func() { view.activate(entry) }),
	}

	if openWith := c.openWithMenuItems(fullPath); len(openWith) > 0 {
		openWithItem := fyne.NewMenuItem("Open With", nil)
		openWithItem.ChildMenu = fyne.NewMenu("", openWith...)
		items = append(items, openWithItem)
	}

	items = append(items,
		fyne.NewMenuItem("Duplicate", func() { c.duplicateEntry(view, fullPath) }),
		fyne.NewMenuItem("Move to Trash", func() { c.trashEntry(view, fullPath) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Copy", func() { c.copyToClipboard(view) }),
		fyne.NewMenuItem("Paste", func() { c.pasteInto(p, view) }),
		fyne.NewMenuItem("Copy Name", func() { c.win.Clipboard().SetContent(entry.Name) }),
		fyne.NewMenuItem("Copy Path", func() { c.win.Clipboard().SetContent(fullPath) }),
		fyne.NewMenuItemSeparator(),
	)

	compressItem := fyne.NewMenuItem("Compress", nil)
	compressItem.ChildMenu = fyne.NewMenu("", c.compressMenuItems(view, c.inactivePaneOf(p))...)
	items = append(items,
		compressItem,
		fyne.NewMenuItem("Multi-Rename Tool…", func() { c.showMultiRenameToolFor(view) }),
		fyne.NewMenuItem("Create Symbolic Link…", func() { c.createSymlink(view, fullPath, c.inactivePaneOf(p)) }),
		fyne.NewMenuItem("Reveal in File Manager", func() {
			if err := launch.RevealInFileManager(fullPath, entry.IsDir); err != nil {
				dialog.ShowError(err, c.win)
			}
		}),
	)

	targetDir := fullPath
	if !entry.IsDir {
		targetDir = view.fs.Dir(fullPath)
	}
	other := c.inactivePaneOf(p)
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Reveal in Opposite Pane", func() { c.navigatePane(other, targetDir) }),
		fyne.NewMenuItem("Reveal in Opposite Pane (New Tab)", func() { other.addTabFromState(panelstate.New(targetDir)) }),
	)

	if entry.IsDir {
		connectionID := ""
		if idFS, ok := view.fs.(hasConnectionID); ok {
			connectionID = idFS.ConnectionID()
		}
		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem(`Add "`+entry.Name+`" to Favorites`, func() {
				c.favorites.Add(entry.Name, fullPath, connectionID)
				c.saveFavorites()
			}),
		)
	}

	widget.NewPopUpMenu(fyne.NewMenu("", items...), c.win.Canvas()).ShowAtPosition(pos)
}

// showArchivedRowContextMenu is the stripped-down context menu for a row
// inside an open archive — most real-filesystem actions above (Open With,
// Duplicate, Trash, Compress, Create Symbolic Link, Reveal in File
// Manager/Opposite Pane, Add to Favorites) don't apply to something that
// only exists inside a read-only .zip.
func (c *commander) showArchivedRowContextMenu(zfs *zipfs.FS, view *fileListView, entry vfs.Entry, fullPath string, p *pane, pos fyne.Position) {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Open", func() { view.activate(entry) }),
	}
	if !entry.IsDir {
		items = append(items, fyne.NewMenuItem("View", func() { c.viewArchivedMember(zfs, entry.Name, fullPath) }))
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Extract to Opposite Pane", func() { c.extractToOppositePane(view, zfs, p) }),
	)
	widget.NewPopUpMenu(fyne.NewMenu("", items...), c.win.Canvas()).ShowAtPosition(pos)
}

// extractToOppositePane is the archive context menu's F5-Extract
// equivalent: the current selection (or just the cursor row, same
// SelectionOrCursor rule as everywhere else) into the opposite pane's
// current directory.
func (c *commander) extractToOppositePane(view *fileListView, zfs *zipfs.FS, p *pane) {
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		return
	}
	dst := c.inactivePaneOf(p).activeState()
	if dst == nil {
		return
	}
	c.runFileOp("Extracting", paths, dst.Path, zfs.Extract, p)
}

// openWithMenuItems lists the user's configured external editors (F9 →
// Editors) as "open this specific file with…" choices — reusing
// internal/editors rather than inventing a separate app-picker, since Fyne
// has no portable "choose an application" dialog anyway.
func (c *commander) openWithMenuItems(path string) []*fyne.MenuItem {
	var items []*fyne.MenuItem
	for _, e := range c.editorConfig.Editors {
		cmd, name := e.Command, e.Name
		items = append(items, fyne.NewMenuItem(name, func() {
			if err := launch.OpenWith(cmd, path); err != nil {
				dialog.ShowError(err, c.win)
			}
		}))
	}
	return items
}

func (c *commander) duplicateEntry(view *fileListView, path string) {
	go func() {
		_, err := fsops.Duplicate(path)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, c.win)
			}
			view.Reload()
		})
	}()
}

// compressMenuItems builds Compress's submenu: "To .zip" (always, stdlib
// archive/zip, no external dependency) and "To .7z" (only when a 7z-capable
// binary is actually usable — see fsops.SevenZipAvailable). Unlike the rest
// of this context menu (which acts on just the right-clicked row),
// Compress acts on the pane's full selection via SelectionOrCursor — the
// same "selection if any, else cursor" rule F5/F6/F8 already use — since
// compressing a multi-selection into one archive is the whole point.
func (c *commander) compressMenuItems(view *fileListView, other *pane) []*fyne.MenuItem {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("To .zip", func() { c.compressSelection(view, "zip", "", other) }),
	}
	if bin, ok := fsops.SevenZipAvailable(c.sevenZipPath); ok {
		items = append(items, fyne.NewMenuItem("To .7z", func() { c.compressSelection(view, "7z", bin, other) }))
	}
	return items
}

// compressSelection defaults to "alongside the source, no prompt" — fast,
// and right nearly always where you'd want it — except from a listbox view
// (see listboxfs) or a remote connection, neither of which has a real
// "alongside" for its entries to share (a listbox view has no single real
// directory; a connection's archive would have to land somewhere real
// anyway, and Compress never writes to a connection): there, it prompts for
// an explicit destination instead of just refusing, defaulting to other
// (the opposite pane)'s current directory, the same cross-pane convention
// F5/F6 already use. A remote source downloads the selection to a temp dir
// first (Download, same as F5 Copy's remote side), then compresses the
// downloaded copies — no separate progress dialog for the download step,
// matching Compress's existing dialog-free "just do it" feel rather than
// introducing one only for this case.
func (c *commander) compressSelection(view *fileListView, ext, sevenZipBin string, other *pane) {
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		return
	}
	remoteSF, isRemote := view.fs.(remoteConnFS)

	runCompress := func(dest string) {
		go func() {
			sources := paths
			var tempDir string
			if isRemote {
				dir, err := os.MkdirTemp("", "krankybear-compress-*")
				if err != nil {
					fyne.Do(func() { dialog.ShowError(err, c.win) })
					return
				}
				tempDir = dir
				if err := remoteSF.Download(paths, tempDir, nil, nil); err != nil {
					os.RemoveAll(tempDir)
					fyne.Do(func() { dialog.ShowError(err, c.win) })
					return
				}
				entries, err := os.ReadDir(tempDir)
				if err != nil {
					os.RemoveAll(tempDir)
					fyne.Do(func() { dialog.ShowError(err, c.win) })
					return
				}
				sources = make([]string, len(entries))
				for i, e := range entries {
					sources[i] = filepath.Join(tempDir, e.Name())
				}
			}

			var err error
			if ext == "7z" {
				err = fsops.CompressSevenZip(sevenZipBin, sources, dest)
			} else {
				err = fsops.Compress(sources, dest)
			}
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				view.Reload()
			})
		}()
	}

	if _, ok := view.fs.(*listboxfs.FS); ok || isRemote {
		destDir := ""
		if other != nil {
			if st := other.activeState(); st != nil {
				destDir = st.Path
			}
		}
		nameEntry := newDialogEntry()
		nameEntry.SetText(fsops.CompressName(destDir, paths, ext))
		content := container.NewVBox(widget.NewLabel("Compress to:"), nameEntry)
		d := dialog.NewCustomConfirm("Compress", "Compress", "Cancel", content, func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			runCompress(nameEntry.Text)
		}, c.win)
		d.Resize(fyne.NewSize(560, 160))
		showDialog(d)
		c.win.Canvas().Focus(nameEntry)
		nameEntry.TypedShortcut(&fyne.ShortcutSelectAll{})
		return
	}

	runCompress(fsops.CompressName(view.CurrentPath(), paths, ext))
}

// createSymlink prompts for where to create a link to sourcePath, defaulting
// to "link-<name>" alongside the source in the tab's own current directory
// (not the opposite pane — a symlink is meant to sit next to what it points
// at, unlike Copy/Move). The default name is pre-selected so retyping it is
// a single keystroke, same convention as F7 MkDir's prefill.
//
// Against a remote connection this only works for SFTP/SMB (symlinkFS —
// SFTP has a real SYMLINK op; SMB's is best-effort via NTFS reparse points,
// may fail against a Samba-backed share) — FileAgent has no such op and
// stays blocked via blockIfRemote, same as before this existed at all.
func (c *commander) createSymlink(view *fileListView, sourcePath string, other *pane) {
	if c.blockIfListbox(view) {
		return
	}
	symFS, remoteSymlinkable := view.fs.(symlinkFS)
	if !remoteSymlinkable && c.blockIfRemote(view) {
		return
	}
	var defaultPath string
	if remoteSymlinkable {
		// fsops.SymlinkName's os.Lstat-based collision loop assumes a real
		// local path — not safe against a presented "scheme://host/..."
		// path, so a remote default just takes the plain suggested name
		// with no collision check; the user can retype it if it happens to
		// collide (the create attempt below will fail with a clear error
		// either way, same as any other name conflict would).
		defaultPath = view.fs.Join(view.CurrentPath(), "link-"+filepath.Base(sourcePath))
	} else {
		defaultPath = fsops.SymlinkName(view.CurrentPath(), filepath.Base(sourcePath))
	}
	nameEntry := newDialogEntry()
	nameEntry.SetText(defaultPath)
	content := container.NewVBox(widget.NewLabel("Create symbolic link at:"), nameEntry)
	d := dialog.NewCustomConfirm("Create Symbolic Link", "Create", "Cancel", content, func(ok bool) {
		if !ok || strings.TrimSpace(nameEntry.Text) == "" {
			return
		}
		var err error
		if remoteSymlinkable {
			err = symFS.Symlink(sourcePath, nameEntry.Text)
		} else {
			err = fsops.Symlink(sourcePath, nameEntry.Text)
		}
		if err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		view.Reload()
		if v := other.activeView(); v != nil {
			v.Reload()
		}
	}, c.win)
	d.Resize(fyne.NewSize(560, 160))
	showDialog(d)
	c.win.Canvas().Focus(nameEntry)
	nameEntry.TypedShortcut(&fyne.ShortcutSelectAll{})
}

func (c *commander) trashEntry(view *fileListView, path string) {
	showDialog(dialog.NewConfirm("Move to Trash", fmt.Sprintf("Send %q to the trash?", filepath.Base(path)), func(ok bool) {
		if !ok {
			return
		}
		go func() {
			err := fsops.Delete([]string{path}, false)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, c.win)
				}
				view.Reload()
			})
		}()
	}, c.win))
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
