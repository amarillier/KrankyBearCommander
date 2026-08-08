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
	// Checked first, before the "skip while typing" guard below: a dialog's
	// auto-focused text entry (Search, F6/F7, ...) IS a *widget.Entry, so
	// Escape needs to reach dismissTopDialog before that guard would
	// otherwise swallow it. dismissTopDialog itself is a no-op (returns
	// false) when no dialog is open, so plain Escape elsewhere is unaffected.
	if ev.Name == fyne.KeyEscape && dismissTopDialog() {
		return
	}

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

	// Copy/Paste real files to/from the OS clipboard (see clipboard_ui.go):
	// same free "defers to a focused Entry" behavior as Select All above,
	// via Fyne's own built-in fyne.ShortcutCopy/ShortcutPaste types.
	c.win.Canvas().AddShortcut(&fyne.ShortcutCopy{}, func(fyne.Shortcut) { c.doCopyToClipboard() })
	c.win.Canvas().AddShortcut(&fyne.ShortcutPaste{}, func(fyne.Shortcut) { c.doPaste() })

	// Multi-Rename Tool (see multirename_ui.go), matching TotalCmd's Ctrl+M
	// convention. Literal Ctrl, not the platform-primary modifier: Cmd+M is
	// already macOS's own Minimize Window shortcut (see this app's own
	// Cmd/Ctrl+M in the Help window) — same reasoning as Ctrl+U above.
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyM, Modifier: desktop.ControlModifier},
		func(fyne.Shortcut) { c.showMultiRenameTool() })

	// Refresh Both Panes and Search — literal Ctrl even on macOS, same
	// reasoning as Ctrl+U/Ctrl+M above (Cmd+R/Cmd+F aren't reserved by
	// macOS itself, but literal Ctrl keeps every custom shortcut in this
	// app on one consistent, collision-free modifier).
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: desktop.ControlModifier},
		func(fyne.Shortcut) { c.doRefresh() })
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: desktop.ControlModifier},
		func(fyne.Shortcut) { c.showSearch(c.activePane()) })

	// Switch Active Pane: plain Tab isn't usable for this — Fyne's glfw
	// driver intercepts it before it ever reaches a shortcut, TypedKey, or
	// dispatchKey at all, unconditionally calling its own FocusNext()/
	// FocusPrevious() cycling instead (see capturesTab in window.go),
	// unless the currently-focused widget specifically opts out via the
	// rare fyne.Tabbable interface — true of essentially none of this
	// app's widgets. Ctrl+Tab sidesteps that (a real modifier, so neither
	// this nor the triggersShortcut bug applies) and matches the same
	// convention browsers/IDEs use for "switch pane/tab" — confirmed
	// working on Windows, but not on macOS, where Ctrl+Tab (and
	// Ctrl+Shift+Tab) is plausibly reserved system-wide for cycling a
	// window's own native tabs, intercepted before Fyne's event loop ever
	// sees it — outside this app's control either way. Ctrl+O ("Other
	// pane") is bound to the same action as a reliable alternative that
	// doesn't depend on that.
	toggleActivePane := func(fyne.Shortcut) { c.toggleActivePane() }
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyTab, Modifier: desktop.ControlModifier}, toggleActivePane)
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: desktop.ControlModifier}, toggleActivePane)

	// Focus the command line (cmdline_ui.go): Ctrl+L, matching the browser
	// convention for "focus the location/command bar" (Firefox/Chrome both
	// use Ctrl+L for their address bar) — unused elsewhere in this app, and
	// a real modifier so it doesn't hit the triggersShortcut bug described
	// above. Shows the bar first if it's currently hidden.
	c.win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyL, Modifier: desktop.ControlModifier},
		func(fyne.Shortcut) {
			if !c.showCmdLine {
				c.toggleShowCmdLine()
			}
			c.win.Canvas().Focus(c.cmdEntry)
		})
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

// keyBarButtonKeepFocus is keyBarButton's counterpart for actions that leave
// something specific focused on purpose, which the trailing Unfocus() would
// otherwise immediately undo:
//   - F9's popup menu, which Fyne has already focused for us by the time
//     ShowAtPosition returns (proven by the fact keyboard-triggered F9
//     already closes cleanly with Escape) — unfocusing after would strand it
//     neither focused (Escape/F9 do nothing) nor closed (needs a second
//     click).
//   - F6/F7, which focus their dialog's text field so typing works
//     immediately (see fileops_ui.go) — same reasoning as the Search
//     toolbar button (paneview.go), which has the same exception.
func keyBarButtonKeepFocus(label, tip string, action func()) *ttwidget.Button {
	b := ttwidget.NewButton(label, action)
	b.SetToolTip(tip)
	return b
}

// buildFunctionKeyBar is the on-screen mirror of registerShortcuts, so mouse
// and keyboard always drive the same code path.
func (c *commander) buildFunctionKeyBar() fyne.CanvasObject {
	canvas := c.win.Canvas()
	return container.NewGridWithColumns(11,
		keyBarButton(canvas, "F1 Help", "Open the Help window", func() { showHelp(c.app) }),
		keyBarButton(canvas, "F2 Refresh", "Re-read both panes' directories from disk and re-scan drives", c.doRefresh),
		keyBarButton(canvas, "F3 View", "View the selected file (read-only)", c.doView),
		keyBarButton(canvas, "F4 Edit", "Edit the selected file in the built-in text editor", c.doEdit),
		keyBarButton(canvas, "F5 Copy", "Copy the selection to the other pane's directory", c.doCopy),
		keyBarButtonKeepFocus("F6 Ren/Move", "Move the selection to the other pane, or rename a single item", c.doMoveOrRename),
		keyBarButtonKeepFocus("F7 MkDir", "Create a new folder in the active pane", c.doMkdir),
		keyBarButton(canvas, "F8 Delete", "Send the selection to the trash", c.doDeleteTrash),
		keyBarButton(canvas, "⇧F8 Del!", "Permanently delete the selection — bypasses the trash, cannot be undone", c.doDeletePermanent),
		keyBarButtonKeepFocus("F9 Menu", "Open the popup menu (new tab, view mode, panel colors, help)", c.doOpenMenu),
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

	driveBarItem := fyne.NewMenuItem("Show Volume/Drive Toolbar", func() { c.toggleDriveBar() })
	driveBarItem.Checked = c.showDriveBar

	cmdLineItem := fyne.NewMenuItem("Show Command Line", func() { c.toggleShowCmdLine() })
	cmdLineItem.Checked = c.showCmdLine

	briefColumnsItem := fyne.NewMenuItem("Brief Columns", nil)
	briefColumnsItem.ChildMenu = c.buildBriefColumnsSubmenu(nil)

	menu := fyne.NewMenu("",
		fyne.NewMenuItem("New Tab (active pane)", func() {
			p := c.activePane()
			p.addTabFromState(panelstate.New(p.defaultHome()))
		}),
		fyne.NewMenuItem("Brief View", func() { c.activePane().setViewMode(panelstate.ViewBrief) }),
		fyne.NewMenuItem("Full View", func() { c.activePane().setViewMode(panelstate.ViewExpanded) }),
		briefColumnsItem,
		fyne.NewMenuItem("Refresh Both Panes (F2 / Ctrl+R)", func() { c.doRefresh() }),
		fyne.NewMenuItem("Switch Active Pane (Ctrl+Tab / Ctrl+O)", func() { c.toggleActivePane() }),
		fyne.NewMenuItem("Swap Panes (Ctrl+U)", func() { c.swapPanes() }),
		fyne.NewMenuItem("Calculate Folder Sizes", func() { c.doCalculateFolderSizes() }),
		fyne.NewMenuItem("Search… (Ctrl+F)", func() { c.showSearch(c.activePane()) }),
		fyne.NewMenuItem("Compare/Synchronize Directories…", func() { c.showCompareSync(comparePrimaryNone) }),
		fyne.NewMenuItem("Copy (Ctrl/Cmd+C)", func() { c.doCopyToClipboard() }),
		fyne.NewMenuItem("Paste (Ctrl/Cmd+V)", func() { c.doPaste() }),
		fyne.NewMenuItem("Multi-Rename Tool… (Ctrl+M)", func() { c.showMultiRenameTool() }),
		fyne.NewMenuItem("Change Attributes…", func() { c.showChangeAttributes() }),
		hiddenFilesItem,
		driveBarItem,
		cmdLineItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Panel Colors…", func() {
			showColorSchemeSettings(c.app, c.win, c.applyColorScheme)
		}),
		editorsItem,
		fyne.NewMenuItem("Connections…", func() { c.showConnections(c.activePane()) }),
		fyne.NewMenuItem("Application Launcher…", func() { c.showLauncherMenu(c.activePane()) }),
		fyne.NewMenuItem("7-Zip Binary Path…", func() { c.showSevenZipSettings() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Export Settings…", func() { c.showExportSettings() }),
		fyne.NewMenuItem("Import Settings…", func() { c.showImportSettings() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Help", func() { showHelp(c.app) }),
		fyne.NewMenuItem("Check for Updates", func() { checkForUpdatesManual(c.app) }),
		fyne.NewMenuItem("About", func() { showAbout(c.app) }),
	)
	pos := fyne.NewPos(c.win.Canvas().Size().Width/2, c.win.Canvas().Size().Height-80)
	widget.NewPopUpMenu(menu, c.win.Canvas()).ShowAtPosition(pos)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
