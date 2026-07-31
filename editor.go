// editor.go — F4's simple built-in text editor: a plain multi-line entry
// with Save/Save As, prompting to save on close if the buffer is dirty. No
// syntax highlighting in Phase 1.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	fynetooltip "github.com/dweymouth/fyne-tooltip"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/editors"
	"commander/internal/launch"
)

// doEdit opens the selected file in whichever editor is currently the
// default (see editors_ui.go / the F9 popup's "Editors" submenu): the
// built-in editor below, or one of the configured external editors, spawned
// detached with the file path as its last argument. Against a remote
// connection, only the built-in editor is offered at all (see
// editRemoteMember) — an external editor runs as a detached process with no
// reliable "it's done, re-upload now" signal, so that case is deferred
// rather than half-supported.
func (c *commander) doEdit() {
	view := c.activePane().activeView()
	if c.blockIfArchive(view) {
		return
	}
	names := view.SelectionOrCursorNames()
	if len(names) == 0 {
		c.showStatus("select a file to edit")
		return
	}
	entry, path, ok := view.entryAndPath(names[0])
	if !ok {
		return
	}
	if entry.IsDir {
		c.showStatus("F4: select a file, not a directory")
		return
	}

	if remoteFS, ok := view.fs.(remoteConnFS); ok {
		c.editRemoteMember(remoteFS, entry.Name, path)
		return
	}
	pane := c.activePane()

	if c.editorConfig.Default != editors.BuiltinName {
		if ed, ok := c.editorConfig.Find(c.editorConfig.Default); ok {
			if err := launch.OpenWith(ed.Command, path); err != nil {
				dialog.ShowError(err, c.win)
			}
			return
		}
		// Configured default no longer exists (removed since last set) —
		// fall through to the built-in editor rather than silently failing.
	}

	showEditor(c.app, c.win, path, func() { pane.activeView().Reload() })
}

// editRemoteMember downloads presentedPath (a file on remoteFS) to a temp
// file in the background, then opens the built-in editor on it — mirroring
// viewer.go's viewRemoteMember (F3 View's own "download to temp first"
// pattern) — except every Save also uploads the temp copy straight back to
// remoteFS, and the temp directory is removed once the editor window
// actually closes rather than left behind (View is read-only and leaves its
// temp file for the OS to eventually reap; Edit's temp copy has served its
// purpose the moment the session ends).
func (c *commander) editRemoteMember(remoteFS remoteConnFS, name, presentedPath string) {
	go func() {
		dir, err := os.MkdirTemp("", "krankybear-edit-*")
		if err != nil {
			fyne.Do(func() { c.showStatus("cannot edit " + name + ": " + err.Error()) })
			return
		}
		err = remoteFS.Download([]string{presentedPath}, dir, nil, nil)
		fyne.Do(func() {
			if err != nil {
				os.RemoveAll(dir)
				c.showStatus("cannot edit " + name + ": " + err.Error())
				return
			}
			tempPath := filepath.Join(dir, name)
			remoteDir := remoteFS.Dir(presentedPath)
			upload := func(localPath string) error {
				return remoteFS.Upload([]string{localPath}, remoteDir, nil, nil)
			}
			showEditorWithUpload(c.app, c.win, tempPath, func() { c.activePane().activeView().Reload() }, upload, func() { os.RemoveAll(dir) })
		})
	}()
}

func showEditor(a fyne.App, parent fyne.Window, path string, onSaved func()) {
	showEditorWithUpload(a, parent, path, onSaved, nil, nil)
}

// showEditorWithUpload is showEditor's real implementation. upload, if set,
// fires after every successful Save that writes back to the ORIGINAL path
// (not after a Save As to some other local path — that's a plain local
// copy, with nothing remote to upload back to). onClosed, if set, fires
// exactly once, when the window is actually closing (after the "save
// before closing?" prompt, whichever way the user answers it) — not after
// each individual Save, since one editor session commonly saves more than
// once before the window closes.
func showEditorWithUpload(a fyne.App, parent fyne.Window, path string, onSaved func(), upload func(localPath string) error, onClosed func()) {
	win := a.NewWindow("Edit: " + filepath.Base(path))
	win.SetIcon(resourceKrankyBearCommanderPng)

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		dialog.ShowError(err, parent)
		return
	}

	entry := widget.NewMultiLineEntry()
	entry.SetText(string(content))
	entry.Wrapping = fyne.TextWrapOff
	entry.TextStyle = fyne.TextStyle{Monospace: true}

	dirty := false
	entry.OnChanged = func(string) { dirty = true }

	originalPath := path
	currentPath := path
	save := func(target string) {
		if err := os.WriteFile(target, []byte(entry.Text), 0o644); err != nil {
			dialog.ShowError(err, win)
			return
		}
		currentPath = target
		dirty = false
		win.SetTitle("Edit: " + filepath.Base(currentPath))
		if upload != nil && target == originalPath {
			if err := upload(target); err != nil {
				dialog.ShowError(fmt.Errorf("upload failed: %w", err), win)
			}
		}
		if onSaved != nil {
			onSaved()
		}
	}

	saveAs := func() {
		fd := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
			if err != nil || w == nil {
				return
			}
			defer w.Close()
			save(w.URI().Path())
		}, win)
		if dir, err := storage.ListerForURI(mustParentURI(currentPath)); err == nil {
			fd.SetLocation(dir)
		}
		fd.Show()
	}

	saveBtn := ttwidget.NewButton("Save", func() { save(currentPath) })
	saveBtn.SetToolTip("Save changes to " + filepath.Base(currentPath))
	saveAsBtn := ttwidget.NewButton("Save As…", saveAs)
	saveAsBtn.SetToolTip("Save a copy under a new name or location")
	closeBtn := ttwidget.NewButton("Close", func() { win.Close() })
	closeBtn.SetToolTip("Close this editor (prompts to save first if there are unsaved changes)")
	toolbar := container.NewHBox(saveBtn, saveAsBtn, closeBtn)

	body := container.NewBorder(toolbar, nil, nil, nil, container.NewScroll(entry))
	win.SetContent(fynetooltip.AddWindowToolTipLayer(body, win.Canvas()))
	win.Resize(fyne.NewSize(800, 600))

	closeUp := func() {
		fynetooltip.DestroyWindowToolTipLayer(win.Canvas())
		win.Close()
		if onClosed != nil {
			onClosed()
		}
	}
	win.SetCloseIntercept(func() {
		if !dirty {
			closeUp()
			return
		}
		dialog.NewConfirm("Unsaved Changes", "Save changes before closing?", func(ok bool) {
			if ok {
				save(currentPath)
			}
			closeUp()
		}, win).Show()
	})

	win.Show()
}

func mustParentURI(path string) fyne.URI {
	return storage.NewFileURI(filepath.Dir(path))
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
