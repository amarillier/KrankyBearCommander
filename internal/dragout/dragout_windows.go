package dragout

// Windows has no equivalent of macOS's NSEvent local monitor, so detecting
// a drag gesture means subclassing the window's own message procedure
// (swapping GWLP_WNDPROC, the standard Win32 technique) to watch
// WM_LBUTTONDOWN/WM_MOUSEMOVE/WM_LBUTTONUP ourselves, then calling
// DoDragDrop synchronously once the mouse crosses the OS drag threshold —
// exactly mirroring the threshold-then-start structure of the macOS
// bridge, just detected a different way. Every message is always forwarded
// to the original window procedure afterward (CallWindowProc), so GLFW's
// own click/keyboard handling keeps working unchanged.
//
// OleInitialize (STA-mode COM) is required for DoDragDrop, but GLFW very
// likely already called it: drag-IN (Window.SetOnDropped) only works at
// all because GLFW registers itself as an OLE drop target via
// RegisterDragDrop, which itself requires STA-mode COM already
// initialized on this thread. Since drag-IN already works in this app on
// Windows, that threading model should already be compatible with our own
// DoDragDrop call — OleInitialize's S_FALSE ("already initialized") is an
// expected, harmless outcome here, not an error.

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	ole32  = windows.NewLazySystemDLL("ole32.dll")

	procCallWindowProcW   = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")

	procOleInitialize = ole32.NewProc("OleInitialize")
	procDoDragDrop    = ole32.NewProc("DoDragDrop")
)

const (
	gwlpWndProc = ^uintptr(4 - 1) // GWLP_WNDPROC (-4) as the uintptr SetWindowLongPtrW expects

	wmLButtonDown = 0x0201
	wmMouseMove   = 0x0200
	wmLButtonUp   = 0x0202

	smCxDrag = 68
	smCyDrag = 69

	dropEffectCopy = 1
)

var (
	mu        sync.Mutex
	activeHit HitTest

	origWndProc uintptr

	downX, downY int32
	dragging     bool
)

// Supported reports whether this platform has a real drag-out
// implementation — true on Windows.
func Supported() bool { return true }

// Install wires up native drag-out support for win, calling ht to decide
// what a click-drag should carry once it crosses the OS drag threshold.
// Must be called after win.Show(): Fyne's GLFW driver creates the real
// HWND lazily on Show(), so RunNative before that hands back a zero/null
// window handle.
func Install(win fyne.Window, ht HitTest) error {
	nw, ok := win.(driver.NativeWindow)
	if !ok {
		return errors.New("dragout: window does not implement driver.NativeWindow")
	}

	mu.Lock()
	activeHit = ht
	mu.Unlock()

	procOleInitialize.Call(0)

	var installErr error
	nw.RunNative(func(ctx any) {
		winCtx, ok := ctx.(driver.WindowsWindowContext)
		if !ok {
			installErr = errors.New("dragout: unexpected native context type")
			return
		}
		cb := windows.NewCallback(wndProcSubclass)
		old, _, _ := procSetWindowLongPtrW.Call(winCtx.HWND, gwlpWndProc, cb)
		origWndProc = old
	})
	return installErr
}

func wndProcSubclass(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmLButtonDown:
		downX, downY = extractCoords(lParam)
		dragging = false
	case wmMouseMove:
		if wParam&mkLButtonFlag != 0 && !dragging {
			x, y := extractCoords(lParam)
			thresholdX, _, _ := procGetSystemMetrics.Call(smCxDrag)
			thresholdY, _, _ := procGetSystemMetrics.Call(smCyDrag)
			if absInt32(x-downX) > int32(thresholdX) || absInt32(y-downY) > int32(thresholdY) {
				dragging = true
				mu.Lock()
				ht := activeHit
				mu.Unlock()
				if ht != nil {
					if paths := ht(fyne.NewPos(float32(downX), float32(downY))); len(paths) > 0 {
						startDrag(paths)
					}
				}
				dragging = false
			}
		}
	case wmLButtonUp:
		dragging = false
	}
	r, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)
	return r
}

// startDrag blocks (DoDragDrop pumps its own message loop internally) until
// the user drops or cancels. src/data are kept in the keep-alive registry
// (see comobjects_windows.go) for exactly that span, since a bare uintptr
// handed to a syscall is invisible to Go's garbage collector.
func startDrag(paths []string) {
	src := newDropSource()
	data := newDataObject(paths)

	var effect uint32
	procDoDragDrop.Call(
		uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(src)),
		dropEffectCopy,
		uintptr(unsafe.Pointer(&effect)),
	)

	keepAliveRemove(unsafe.Pointer(src))
	keepAliveRemove(unsafe.Pointer(data))
}

func extractCoords(lParam uintptr) (int32, int32) {
	x := int32(int16(uint16(lParam & 0xFFFF)))
	y := int32(int16(uint16((lParam >> 16) & 0xFFFF)))
	return x, y
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
