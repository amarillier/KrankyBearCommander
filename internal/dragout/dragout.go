// Package dragout starts a native OS drag session when the user click-drags
// files out of a pane toward another app (Finder/Explorer/Nautilus) — the
// reverse of drag-IN (see dragdrop_ui.go, which uses Fyne's own portable
// Window.SetOnDropped API). Fyne has no equivalent API for INITIATING an
// OS-level drag, so this needs real native code per platform; currently
// implemented for macOS only (an NSDraggingSession via a small Cocoa
// bridge, reached through Fyne's driver.NativeWindow). Windows/Linux are
// no-ops for now — Supported() reports false and Install does nothing.
package dragout

import "fyne.io/fyne/v2"

// HitTest is called synchronously on the main thread when a click-drag
// inside the window's content area crosses the OS drag threshold, before
// any native drag session begins. pos is a window-content-relative
// position, top-left origin — the same convention Window.SetOnDropped
// already uses elsewhere in this app. Return the absolute file paths to
// drag, or nil/empty to leave the gesture as an ordinary click (no native
// drag starts).
type HitTest func(pos fyne.Position) []string
