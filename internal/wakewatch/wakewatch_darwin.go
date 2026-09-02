package wakewatch

/*
#cgo LDFLAGS: -framework Cocoa
#include "bridge_darwin.h"
*/
import "C"

import "sync"

var (
	mu     sync.Mutex
	onWake func()
)

// Supported reports whether this platform can detect OS wake-from-sleep —
// true on macOS.
func Supported() bool { return true }

// Install registers cb to be called every time this Mac wakes from sleep.
// cb is invoked from Cocoa's own notification queue, off Fyne's main
// goroutine — wrap any UI work in fyne.Do (CLAUDE.md "fyne.Do is
// mandatory"). Only one callback is kept; a second call replaces the
// first rather than adding a second observer.
func Install(cb func()) {
	mu.Lock()
	onWake = cb
	mu.Unlock()
	C.wakewatch_install()
}

// wakewatchOnWake is called from bridge_darwin.m's NSWorkspace observer
// each time the Mac wakes from sleep.
//
//export wakewatchOnWake
func wakewatchOnWake() {
	mu.Lock()
	cb := onWake
	mu.Unlock()
	if cb != nil {
		cb()
	}
}
