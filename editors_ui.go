// editors_ui.go — the F4 editor preference: built-in or one of a
// user-configured list of external editors. internal/editors owns
// persistence; this file is the Fyne-facing half (the F9 popup menu's
// "Editors" submenu, and "Manage Editors…").
package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/editors"
)

func (c *commander) editorsPath() string {
	p, err := editors.DefaultPath(appName)
	if err != nil {
		return ""
	}
	return p
}

func (c *commander) loadEditors() {
	path := c.editorsPath()
	if path == "" {
		return
	}
	if cfg, err := editors.Load(path); err == nil {
		c.editorConfig = cfg
	}
}

func (c *commander) saveEditors() {
	path := c.editorsPath()
	if path == "" {
		return
	}
	_ = editors.Save(path, c.editorConfig)
}

func (c *commander) setDefaultEditor(name string) {
	c.editorConfig.Default = name
	c.saveEditors()
}

// buildEditorsSubmenu lists Built-in plus every configured external editor
// (checkmarking the current default) — tapping one switches F4's default
// without opening anything.
func (c *commander) buildEditorsSubmenu() *fyne.Menu {
	builtin := fyne.NewMenuItem(editors.BuiltinName, func() { c.setDefaultEditor(editors.BuiltinName) })
	builtin.Checked = c.editorConfig.Default == editors.BuiltinName
	items := []*fyne.MenuItem{builtin}

	for _, e := range c.editorConfig.Editors {
		name := e.Name
		item := fyne.NewMenuItem(name, func() { c.setDefaultEditor(name) })
		item.Checked = c.editorConfig.Default == name
		items = append(items, item)
	}

	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Manage Editors…", func() { c.showManageEditors() }),
	)
	return fyne.NewMenu("", items...)
}

// showManageEditors lists configured external editors (with Remove buttons)
// and a small form to add a new one (name + launch command; F4 appends the
// file path as that command's last argument).
func (c *commander) showManageEditors() {
	list := container.NewVBox()

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Name (e.g. VS Code)")
	cmdEntry := widget.NewEntry()
	cmdEntry.SetPlaceHolder("Command (e.g. code, or /usr/local/bin/subl)")

	var refresh func()

	addBtn := widget.NewButton("Add", func() {
		name := strings.TrimSpace(nameEntry.Text)
		cmd := strings.TrimSpace(cmdEntry.Text)
		if name == "" || cmd == "" || name == editors.BuiltinName {
			return
		}
		c.editorConfig.Add(name, cmd)
		c.saveEditors()
		nameEntry.SetText("")
		cmdEntry.SetText("")
		refresh()
	})

	refresh = func() {
		var rows []fyne.CanvasObject
		if len(c.editorConfig.Editors) == 0 {
			rows = append(rows, widget.NewLabel("No external editors configured yet."))
		}
		for _, e := range c.editorConfig.Editors {
			name := e.Name
			removeBtn := widget.NewButton("Remove", func() {
				c.editorConfig.Remove(name)
				c.saveEditors()
				refresh()
			})
			row := container.NewBorder(nil, nil, nil, removeBtn, widget.NewLabel(e.Name+"  —  "+e.Command))
			rows = append(rows, row)
		}
		list.Objects = rows
		list.Refresh()
	}
	refresh()

	content := container.NewVBox(
		container.NewVScroll(list),
		widget.NewSeparator(),
		widget.NewLabel("Add a new external editor:"),
		nameEntry,
		cmdEntry,
		addBtn,
	)

	d := dialog.NewCustom("Manage Editors", "Close", content, c.win)
	d.Resize(fyne.NewSize(480, 480))
	d.Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
