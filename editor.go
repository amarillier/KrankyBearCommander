// editor.go — F4's simple built-in text editor: a plain multi-line entry
// with Save/Save As, prompting to save on close if the buffer is dirty. No
// syntax highlighting in Phase 1.
package main

import (
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
// detached with the file path as its last argument.
func (c *commander) doEdit() {
	paths := c.activePane().activeView().SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("select a file to edit")
		return
	}
	path := paths[0]
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		c.showStatus("F4: select a file, not a directory")
		return
	}

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

	pane := c.activePane()
	showEditor(c.app, c.win, path, func() { pane.activeView().Reload() })
}

func showEditor(a fyne.App, parent fyne.Window, path string, onSaved func()) {
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

	currentPath := path
	save := func(target string) {
		if err := os.WriteFile(target, []byte(entry.Text), 0o644); err != nil {
			dialog.ShowError(err, win)
			return
		}
		currentPath = target
		dirty = false
		win.SetTitle("Edit: " + filepath.Base(currentPath))
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

	win.SetCloseIntercept(func() {
		if !dirty {
			fynetooltip.DestroyWindowToolTipLayer(win.Canvas())
			win.Close()
			return
		}
		dialog.NewConfirm("Unsaved Changes", "Save changes before closing?", func(ok bool) {
			if ok {
				save(currentPath)
			}
			fynetooltip.DestroyWindowToolTipLayer(win.Canvas())
			win.Close()
		}, win).Show()
	})

	win.Show()
}

func mustParentURI(path string) fyne.URI {
	return storage.NewFileURI(filepath.Dir(path))
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
