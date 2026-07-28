// dialogesc_ui.go — lets Escape cancel/close whichever dialog is currently
// on top, exactly as if its own Cancel/Close button were clicked, instead of
// requiring an explicit click every time (F6/F7/F8/Search and friends were
// the reported cases, but this applies broadly).
//
// Two distinct mechanisms are needed, not one:
//   - dispatchKey (keymap.go), via the canvas's raw TypedKey stream, for
//     when NOTHING is focused — true for most dialogs (dialog.NewConfirm
//     doesn't auto-focus anything).
//   - dialogEntry below, for when a dialog's auto-focused Entry (Search,
//     Multi-Rename, F6/F7, ...) IS focused: Fyne's glfw driver hands a
//     focused widget's own TypedKey the whole key stream INSTEAD of the
//     canvas's OnTypedKey callback (see window.go's key routing) — so
//     dispatchKey is never even called while an Entry has focus, no matter
//     what key is pressed. This is exactly the "triggersShortcut bug"
//     dispatchKey's own doc comment describes for the F-keys, just one
//     level earlier: canvas.AddShortcut can't help either, since Escape
//     (no modifier) hits that same bug. The only reliable place left to
//     catch it is the focused widget itself — same fix shape rename_ui.go's
//     renameEntry already uses for inline-rename's Escape-to-cancel.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// openDialog pairs a shown dialog.Dialog with the action Escape should
// trigger for it.
type openDialog struct {
	d       dialog.Dialog
	dismiss func()
}

// openDialogs is a stack (last shown = topmost = what Escape affects first),
// since dialogs do nest here (e.g. the File Exists conflict dialog appears
// on top of a Copy/Move progress dialog).
var openDialogs []openDialog

// showDialog shows d and registers it so Escape dismisses it exactly like
// its own Cancel/Close button: dialog.Dismiss() triggers the same ok=false
// callback Cancel's own button uses, for any ordinary Confirm/Custom dialog.
func showDialog(d dialog.Dialog) {
	showDialogWithDismiss(d, d.Dismiss)
}

// showDialogWithDismiss is showDialog for a dialog whose Cancel doesn't run
// through the dialog framework's own ok/cancel callback — e.g. the File
// Exists conflict dialog (Overwrite/Skip/Rename/Cancel report through a
// channel, not a callback) or Calculate Folder Sizes' progress dialog
// (Cancel just flags a bool the background goroutine polls, and — unlike
// every other dialog here — deliberately does NOT close the dialog
// immediately, since the existing Cancel button doesn't either). dismiss
// should reach whatever that dialog's own Cancel button does, so Escape is
// indistinguishable from clicking it.
func showDialogWithDismiss(d dialog.Dialog, dismiss func()) {
	openDialogs = append(openDialogs, openDialog{d: d, dismiss: dismiss})
	d.SetOnClosed(func() { popDialog(d) })
	d.Show()
}

// popDialog removes d from the stack — called via SetOnClosed above, so it
// fires however the dialog actually closes (Escape, or its own button).
func popDialog(d dialog.Dialog) {
	for i, e := range openDialogs {
		if e.d == d {
			openDialogs = append(openDialogs[:i], openDialogs[i+1:]...)
			return
		}
	}
}

// dismissTopDialog is Escape's entry point (see dispatchKey): dismisses
// whichever dialog is currently topmost and reports whether there was one,
// so Escape falls through to do nothing when no dialog is open.
func dismissTopDialog() bool {
	if len(openDialogs) == 0 {
		return false
	}
	openDialogs[len(openDialogs)-1].dismiss()
	return true
}

// dialogEntry is widget.Entry plus one addition: Escape reaches
// dismissTopDialog(), same as every other key it already handles (Home/End,
// arrows, Backspace, ...) — see this file's top doc comment for why a plain
// widget.Entry can't rely on dispatchKey for this. Use this instead of
// widget.NewEntry() for any text field inside a dialog wired up via
// showDialog/showDialogWithDismiss, so Escape works no matter which field
// (if more than one) currently has focus.
type dialogEntry struct {
	widget.Entry
}

func newDialogEntry() *dialogEntry {
	e := &dialogEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *dialogEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		dismissTopDialog()
		return
	}
	e.Entry.TypedKey(ev)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
