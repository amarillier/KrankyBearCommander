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
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/fsops"
	"commander/internal/launch"
	"commander/internal/panelstate"
)

// showRowContextMenu builds and shows the popup for name (already resolved
// to a real row — offerContextMenu excludes "" and "..").
func (c *commander) showRowContextMenu(p *pane, view *fileListView, name string, pos fyne.Position) {
	entry, fullPath, ok := view.entryAndPath(name)
	if !ok {
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
		fyne.NewMenuItem("Copy Name", func() { c.win.Clipboard().SetContent(entry.Name) }),
		fyne.NewMenuItem("Copy Path", func() { c.win.Clipboard().SetContent(fullPath) }),
		fyne.NewMenuItemSeparator(),
	)

	compressItem := fyne.NewMenuItem("Compress", nil)
	compressItem.ChildMenu = fyne.NewMenu("", c.compressMenuItems(view)...)
	items = append(items,
		compressItem,
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
		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem(`Add "`+entry.Name+`" to Favorites`, func() {
				c.favorites.Add(entry.Name, fullPath)
				c.saveFavorites()
			}),
		)
	}

	widget.NewPopUpMenu(fyne.NewMenu("", items...), c.win.Canvas()).ShowAtPosition(pos)
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
func (c *commander) compressMenuItems(view *fileListView) []*fyne.MenuItem {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("To .zip", func() { c.compressSelection(view, "zip", "") }),
	}
	if bin, ok := fsops.SevenZipAvailable(c.sevenZipPath); ok {
		items = append(items, fyne.NewMenuItem("To .7z", func() { c.compressSelection(view, "7z", bin) }))
	}
	return items
}

func (c *commander) compressSelection(view *fileListView, ext, sevenZipBin string) {
	paths := view.SelectionOrCursor()
	if len(paths) == 0 {
		return
	}
	dest := fsops.CompressName(view.CurrentPath(), paths, ext)
	go func() {
		var err error
		if ext == "7z" {
			err = fsops.CompressSevenZip(sevenZipBin, paths, dest)
		} else {
			err = fsops.Compress(paths, dest)
		}
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, c.win)
			}
			view.Reload()
		})
	}()
}

// createSymlink prompts for where to create a link to sourcePath, defaulting
// to the opposite pane's current directory with the same base name —
// mirroring F5/F6's "operations target the other pane" convention.
func (c *commander) createSymlink(view *fileListView, sourcePath string, other *pane) {
	defaultPath := sourcePath
	if dst := other.activeState(); dst != nil {
		defaultPath = filepath.Join(dst.Path, filepath.Base(sourcePath))
	}
	nameEntry := widget.NewEntry()
	nameEntry.SetText(defaultPath)
	content := container.NewVBox(widget.NewLabel("Create symbolic link at:"), nameEntry)
	dialog.NewCustomConfirm("Create Symbolic Link", "Create", "Cancel", content, func(ok bool) {
		if !ok || strings.TrimSpace(nameEntry.Text) == "" {
			return
		}
		if err := fsops.Symlink(sourcePath, nameEntry.Text); err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		view.Reload()
		if v := other.activeView(); v != nil {
			v.Reload()
		}
	}, c.win).Show()
}

func (c *commander) trashEntry(view *fileListView, path string) {
	dialog.NewConfirm("Move to Trash", fmt.Sprintf("Send %q to the trash?", filepath.Base(path)), func(ok bool) {
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
	}, c.win).Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
