//go:build !darwin && !windows && !linux

package dragout

import "fyne.io/fyne/v2"

// Supported reports whether this platform has a real drag-out
// implementation — false everywhere except macOS, Windows, and Linux
// (X11/XDND) for now.
func Supported() bool { return false }

// Install is a no-op on platforms without a drag-out implementation.
func Install(fyne.Window, HitTest) error { return nil }
