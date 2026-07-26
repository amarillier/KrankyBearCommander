package dragout

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "bridge_darwin.h"
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

var (
	mu         sync.Mutex
	activeHit  HitTest
	installErr error
)

// Supported reports whether this platform has a real drag-out
// implementation — true on macOS.
func Supported() bool { return true }

// Install wires up native drag-out support for win, calling ht to decide
// what a click-drag should carry once it crosses the OS drag threshold.
// Must be called after win.Show(): Fyne's GLFW driver creates the real
// NSWindow lazily on Show(), so RunNative before that hands back a
// zero/null window handle.
func Install(win fyne.Window, ht HitTest) error {
	nw, ok := win.(driver.NativeWindow)
	if !ok {
		return errors.New("dragout: window does not implement driver.NativeWindow")
	}

	mu.Lock()
	activeHit = ht
	mu.Unlock()

	installErr = nil
	nw.RunNative(func(ctx any) {
		mac, ok := ctx.(driver.MacWindowContext)
		if !ok {
			installErr = errors.New("dragout: unexpected native context type")
			return
		}
		// go vet flags this as a possible unsafe.Pointer misuse, but it's
		// the documented, only way to use driver.MacWindowContext: Fyne
		// deliberately hands back the live NSWindow as a uintptr (to keep
		// cgo types out of its public API) and expects callers to convert
		// it back to a pointer in their own cgo code, right here.
		C.dragout_install(unsafe.Pointer(uintptr(mac.NSWindow)))
	})
	return installErr
}

// dragoutHitTest is called from bridge_darwin.m once a click-drag crosses
// the OS drag threshold. x/y are window-content coordinates, already
// flipped to Fyne's top-left-origin convention. Returns a NUL-separated
// buffer of absolute file paths (caller must free with C's free) and sets
// *count, or returns NULL with *count 0 if there's nothing to drag.
//
//export dragoutHitTest
func dragoutHitTest(x, y C.double, count *C.int) *C.char {
	mu.Lock()
	ht := activeHit
	mu.Unlock()
	*count = 0
	if ht == nil {
		return nil
	}

	paths := ht(fyne.NewPos(float32(x), float32(y)))
	if len(paths) == 0 {
		return nil
	}

	*count = C.int(len(paths))
	return C.CString(strings.Join(paths, "\x00") + "\x00")
}
