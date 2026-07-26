package dragout

// Linux has no equivalent of macOS's NSEvent local monitor or Windows'
// window-message subclassing — X11 drag-and-drop (XDND) has no OS-provided
// "run the whole drag for me" call like NSDraggingSession/DoDragDrop
// either. The *source* application (us) must grab the pointer itself once
// a drag starts and manually track which window is under the cursor,
// speaking a multi-step ClientMessage handshake with whatever's there
// (XdndEnter -> XdndPosition -> wait for XdndStatus -> ... -> XdndDrop ->
// wait for XdndFinished), then answer a SelectionRequest for the actual
// file list, exactly like serving clipboard data. See xdnd_linux.go for
// all of that; this file is just the Fyne-facing entry point plus the
// "has a drag started?" detection, kept as low-risk as possible:
//
// Rather than selecting input events on our own X11 connection (a second,
// independent connection to the same server, since Fyne's driver.
// X11WindowContext only exposes the window ID, not GLFW's own Display*) —
// which risks subtly interacting with GLFW's own event handling in ways I
// have no way to verify — this just periodically polls XQueryPointer
// (idle_poll below), a stateless, non-blocking query that needs no event
// selection or grab at all. Only once a real drag is confirmed (button
// down inside our window, then moved past the OS drag threshold) does
// this hand off to xdnd_linux.go's startDrag, which does grab the pointer
// for the deliberately short remainder of the gesture.
//
// Scoped deliberately narrow for a first pass: one MIME type
// (text/uri-list, XDND's standard file-drop convention), copy action
// only, default cursor (no custom drag icon).

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// pollInterval is how often idlePoll checks XQueryPointer while no drag is
// in progress — frequent enough to feel responsive, cheap enough (one
// round-trip query) not to matter.
const pollInterval = 16 * time.Millisecond

var (
	activeHit HitTest
	ourWindow uintptr
)

// Supported reports whether this platform has a real drag-out
// implementation. Deliberately false for now, despite the XDND
// implementation below being real and mostly working: detection, target
// walking, and serving the actual file data (text/uri-list) via
// SelectionRequest all confirmed working against a real GTK/Nautilus
// target in testing (2026-07-26, Ubuntu/GNOME/X11) — but the target never
// sends XdndStatus back, so the handshake never reaches XdndDrop and no
// file is ever actually dropped. Diagnosing further needs raw X11
// protocol tracing (xtrace/Wireshark) to see what the target really sends,
// which wasn't pursued given Linux is the lowest-priority platform here.
// Flip this back to true (and pick the investigation up from there) if
// revisiting this later.
func Supported() bool { return false }

// Install wires up native drag-out support for win, calling ht to decide
// what a click-drag should carry once it crosses the OS drag threshold.
// Must be called after win.Show(): Fyne's GLFW driver creates the real X11
// window lazily on Show(), so RunNative before that hands back a
// zero/null window handle.
func Install(win fyne.Window, ht HitTest) error {
	nw, ok := win.(driver.NativeWindow)
	if !ok {
		return errNotNativeWindow
	}

	var installErr error
	nw.RunNative(func(ctx any) {
		x11Ctx, ok := ctx.(driver.X11WindowContext)
		if !ok {
			log.Printf("dragout: RunNative context was %T, not driver.X11WindowContext", ctx)
			installErr = errUnexpectedContext
			return
		}
		if err := xdndOpenDisplay(); err != nil {
			log.Println("dragout: xdndOpenDisplay failed:", err)
			installErr = err
			return
		}
		ourWindow = x11Ctx.WindowHandle
		activeHit = ht
		log.Printf("dragout: installed, window=%#x", ourWindow)
		go idlePoll()
	})
	return installErr
}

// mainThreadHitTest calls activeHit on Fyne's own main goroutine and
// blocks until it's done: activeHit reads fileListView selection/widget
// state, which (per this project's own fyne.Do convention — see
// CLAUDE.md) must not be touched from idlePoll's background goroutine.
func mainThreadHitTest(pos fyne.Position) []string {
	var result []string
	done := make(chan struct{})
	fyne.Do(func() {
		if activeHit != nil {
			result = activeHit(pos)
		}
		close(done)
	})
	<-done
	return result
}

// idlePoll watches for "button down inside our window, then moved past the
// OS drag threshold" via plain polling — see the package doc above for why
// this doesn't select input events or grab anything itself.
func idlePoll() {
	var (
		tracking    bool
		dragging    bool
		downRootX   int32
		downRootY   int32
		downWinX    int32
		downWinY    int32
		thresholdSq int32
	)

	for {
		time.Sleep(pollInterval)

		rootX, rootY, winX, winY, winW, winH, button1Down, ok := xdndQueryPointer(ourWindow)
		if !ok {
			continue
		}

		if !button1Down {
			tracking = false
			dragging = false
			continue
		}

		if dragging {
			continue // already handed off to startDrag for this press
		}

		if !tracking {
			if winX >= 0 && winX < winW && winY >= 0 && winY < winH {
				tracking = true
				downRootX, downRootY = rootX, rootY
				downWinX, downWinY = winX, winY
				thresholdSq = dragThresholdSquared()
				log.Printf("dragout: button1 down inside window at win=(%d,%d) root=(%d,%d)", winX, winY, rootX, rootY)
			}
			continue
		}

		dx := rootX - downRootX
		dy := rootY - downRootY
		if dx*dx+dy*dy < thresholdSq {
			continue
		}

		log.Printf("dragout: threshold crossed (moved %d,%d from down point), calling hit test", dx, dy)
		paths := mainThreadHitTest(fyne.NewPos(float32(downWinX), float32(downWinY)))
		if len(paths) == 0 {
			log.Println("dragout: hit test returned nothing to drag")
			continue // nothing to drag from here; wait for the next press
		}

		log.Printf("dragout: starting drag of %d path(s): %v", len(paths), paths)
		dragging = true
		startDrag(ourWindow, paths)
		log.Println("dragout: startDrag returned")
		dragging = false
		tracking = false
	}
}
