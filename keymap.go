// keymap.go — the classic F1-F10 function-key row: both the on-screen button
// bar and the real keyboard shortcuts call the exact same handler methods,
// so relabeling/remapping later (Phase 3) only touches one place per action.
//
// Keyboard dispatch deliberately does NOT use fyne's canvas.AddShortcut /
// desktop.CustomShortcut — Fyne v2.7.4's glfw driver only ever builds a
// CustomShortcut when a *non-Shift* modifier is held (window.go's
// triggersShortcut: `modifier != 0 && ... && modifier != fyne.KeyModifierShift`),
// so a shortcut registered with no modifier (all our F-keys, Enter) or with
// Shift alone (Shift+F8) can simply never fire — see fyne-io/fyne#4393. We
// instead hook the canvas's raw TypedKey stream directly, which every key
// reaches regardless of that bug. The one casualty: fyne.KeyEvent carries no
// modifier state at all, so Shift+F8 (permanent delete) can't be told apart
// from plain F8 this way — it's mouse/menu-only (the ⇧F8 button below, or
// the popup menu), which is arguably fitting for a deliberately-hard-to-hit
// "bypass the trash" action anyway.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/panelstate"
)

// fkeyActions maps every key this app binds globally to its handler. Shared
// by registerShortcuts (the canvas-level fallback for when nothing is
// focused) and keyTable's onOtherKey (for when a table row IS focused — see
// filelist.go's keyTable doc comment).
func (c *commander) fkeyActions() map[fyne.KeyName]func() {
	return map[fyne.KeyName]func(){
		fyne.KeyF1:     func() { showHelp(c.app) },
		fyne.KeyF2:     c.doRefresh,
		fyne.KeyF3:     c.doView,
		fyne.KeyF4:     c.doEdit,
		fyne.KeyF5:     c.doCopy,
		fyne.KeyF6:     c.doMoveOrRename,
		fyne.KeyF7:     c.doMkdir,
		fyne.KeyF8:     c.doDeleteTrash,
		fyne.KeyF9:     c.doOpenMenu,
		fyne.KeyF10:    func() { fyne.Do(func() { quitApp(c.app, c.win) }) },
		fyne.KeyReturn: c.doActivateCursor,
		fyne.KeyEnter:  c.doActivateCursor, // numpad Enter
	}
}

// dispatchKey is the single entry point every keypress funnels through,
// regardless of whether it arrived via the canvas-level SetOnTypedKey
// fallback (nothing focused) or keyTable's onOtherKey (a table row focused).
func (c *commander) dispatchKey(ev *fyne.KeyEvent) {
	action, ok := c.fkeyActions()[ev.Name]
	if !ok {
		return
	}
	// Skip while the user is actively typing in a text field (rename/mkdir
	// dialog, the editor) so e.g. F5 or Enter mid-edit doesn't also trigger
	// a file operation behind/around that dialog. Everywhere else
	// (including nil focus, the common case) these act as global
	// accelerators.
	if _, isEntry := c.win.Canvas().Focused().(*widget.Entry); isEntry {
		return
	}
	action()
}

func (c *commander) registerShortcuts() {
	c.win.Canvas().SetOnTypedKey(c.dispatchKey)

	// Ctrl (a real, non-Shift modifier) doesn't hit the triggersShortcut bug
	// described above, so Ctrl+U can safely use the normal AddShortcut path
	// for "swap panes" (classic dual-pane-commander binding).
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyU, Modifier: desktop.ControlModifier},
		func(fyne.Shortcut) { c.swapPanes() })

	// Select All: Fyne's glfw driver already resolves fyne.ShortcutSelectAll
	// to the platform's own primary modifier (Ctrl+A on Windows/Linux, Cmd+A
	// on macOS — see window.go's triggersShortcut/ctrlMod), and only ever
	// hands it to the canvas (rather than a focused Entry, e.g. a rename
	// dialog) when nothing Shortcutable currently has focus — the same
	// "don't hijack while typing" guarantee dispatchKey enforces manually
	// for the F-keys, here for free.
	c.win.Canvas().AddShortcut(&fyne.ShortcutSelectAll{}, func(fyne.Shortcut) { c.selectAllActive() })

	// Deselect All has no Fyne built-in shortcut type, so register the
	// literal Ctrl+Shift+A / Cmd+Shift+A combos directly (both modifiers are
	// real, non-Shift-alone, so — like Ctrl+U — neither hits the
	// triggersShortcut bug).
	deselect := func(fyne.Shortcut) { c.deselectAllActive() }
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyA, Modifier: desktop.ControlModifier | desktop.ShiftModifier}, deselect)
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyA, Modifier: desktop.SuperModifier | desktop.ShiftModifier}, deselect)
}

// keyBarButton builds one function-key bar button with a tooltip explaining
// what it does — these single-word labels aren't always self-explanatory,
// especially F9's popup menu and Shift+F8's "skip the trash" behavior. It
// also clears keyboard focus after the button fires: Fyne buttons take
// keyboard focus on click and, like keyTable, would otherwise swallow the
// next unmodified keypress (e.g. an F-key) instead of letting it reach
// dispatchKey — see keymap.go's top doc comment.
func keyBarButton(canvas fyne.Canvas, label, tip string, action func()) *ttwidget.Button {
	b := ttwidget.NewButton(label, func() {
		action()
		canvas.Unfocus()
	})
	b.SetToolTip(tip)
	return b
}

// buildFunctionKeyBar is the on-screen mirror of registerShortcuts, so mouse
// and keyboard always drive the same code path.
func (c *commander) buildFunctionKeyBar() fyne.CanvasObject {
	canvas := c.win.Canvas()
	return container.NewGridWithColumns(11,
		keyBarButton(canvas, "F1 Help", "Open the Help window", func() { showHelp(c.app) }),
		keyBarButton(canvas, "F2 Refresh", "Re-read the active pane's directory from disk", c.doRefresh),
		keyBarButton(canvas, "F3 View", "View the selected file (read-only)", c.doView),
		keyBarButton(canvas, "F4 Edit", "Edit the selected file in the built-in text editor", c.doEdit),
		keyBarButton(canvas, "F5 Copy", "Copy the selection to the other pane's directory", c.doCopy),
		keyBarButton(canvas, "F6 Ren/Move", "Move the selection to the other pane, or rename a single item", c.doMoveOrRename),
		keyBarButton(canvas, "F7 MkDir", "Create a new folder in the active pane", c.doMkdir),
		keyBarButton(canvas, "F8 Delete", "Send the selection to the trash", c.doDeleteTrash),
		keyBarButton(canvas, "⇧F8 Del!", "Permanently delete the selection — bypasses the trash, cannot be undone", c.doDeletePermanent),
		keyBarButton(canvas, "F9 Menu", "Open the popup menu (new tab, view mode, panel colors, help)", c.doOpenMenu),
		keyBarButton(canvas, "F10 Quit", "Quit "+appName, func() { fyne.Do(func() { quitApp(c.app, c.win) }) }),
	)
}

// doActivateCursor is Enter's handler: open/navigate into the active pane's
// cursor row, same as a double-click (the Entry-focus guard already applied
// in registerShortcuts covers "don't hijack Enter while typing in a dialog").
func (c *commander) doActivateCursor() {
	if v := c.activePane().activeView(); v != nil {
		v.ActivateCursor()
	}
}

// doOpenMenu is F9's GUI analog to MC's text-mode pull-down-menu key: Fyne
// has no portable way to programmatically pop open the native OS menu bar,
// so this shows a small popup menu with the actions that don't already have
// their own function key.
func (c *commander) doOpenMenu() {
	editorsItem := fyne.NewMenuItem("Editors", nil)
	editorsItem.ChildMenu = c.buildEditorsSubmenu()

	hiddenFilesItem := fyne.NewMenuItem("Show Hidden Files", func() { c.toggleHiddenFiles() })
	hiddenFilesItem.Checked = c.showHiddenFiles

	menu := fyne.NewMenu("",
		fyne.NewMenuItem("New Tab (active pane)", func() {
			p := c.activePane()
			p.addTabFromState(panelstate.New(p.defaultHome()))
		}),
		fyne.NewMenuItem("Brief View", func() { c.activePane().setViewMode(panelstate.ViewBrief) }),
		fyne.NewMenuItem("Full View", func() { c.activePane().setViewMode(panelstate.ViewExpanded) }),
		fyne.NewMenuItem("Refresh (F2)", func() { c.doRefresh() }),
		fyne.NewMenuItem("Swap Panes (Ctrl+U)", func() { c.swapPanes() }),
		fyne.NewMenuItem("Calculate Folder Sizes", func() { c.doCalculateFolderSizes() }),
		fyne.NewMenuItem("Search…", func() { c.showSearch(c.activePane()) }),
		hiddenFilesItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Panel Colors…", func() {
			showColorSchemeSettings(c.app, c.win, c.applyColorScheme)
		}),
		editorsItem,
		fyne.NewMenuItem("7-Zip Binary Path…", func() { c.showSevenZipSettings() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Help", func() { showHelp(c.app) }),
		fyne.NewMenuItem("About", func() { showAbout(c.app) }),
	)
	pos := fyne.NewPos(c.win.Canvas().Size().Width/2, c.win.Canvas().Size().Height-80)
	widget.NewPopUpMenu(menu, c.win.Canvas()).ShowAtPosition(pos)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
