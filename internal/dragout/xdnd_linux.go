package dragout

// XDND (the X11 drag-and-drop protocol) source-side implementation — see
// dragout_linux.go's package doc for the overall shape. This file holds
// all the raw Xlib plumbing.
//
// Most of X11's "give me this event's fields" and "build and send this
// event" operations go through tiny C helper functions in the cgo preamble
// below, rather than Go-side unsafe casts of the XEvent union: cgo's exact
// Go-side representation of a C union isn't something worth guessing at
// when there is no way to compile-check this file locally (it needs cgo +
// X11 headers this machine doesn't have — see the build-server note this
// was written for). A handful of small, single-purpose C functions with
// plain scalar/pointer signatures is easier to get right by inspection
// than hand-rolled union-layout casts would be.
//
// Scoped deliberately narrow, matching dragout_linux.go: one MIME type
// (text/uri-list), copy action only, a bounded wait for XdndFinished
// rather than waiting forever if a target never responds.

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/X.h>
#include <string.h>
#include <sys/select.h>
#include <sys/time.h>

static XErrorHandler kb_prev_error_handler;

// Xlib's default error handler calls exit() on any protocol error — fatal
// for something as inherently racy as "query a window that might close
// mid-drag." BadWindow specifically is expected and swallowed; anything
// else is forwarded to whatever handler was previously installed (e.g.
// GLFW's own), the same "always forward" principle as this package's
// Windows/macOS counterparts.
static int kb_error_handler(Display *d, XErrorEvent *e) {
    if (e->error_code == BadWindow) {
        return 0;
    }
    if (kb_prev_error_handler) {
        return kb_prev_error_handler(d, e);
    }
    return 0;
}

static void kb_install_error_handler(void) {
    kb_prev_error_handler = XSetErrorHandler(kb_error_handler);
}

// Combined XQueryPointer + XGetWindowAttributes for dragout_linux.go's
// idle poller: one round trip per tick rather than two.
static int kb_query_pointer(Display *d, Window w, int *rootX, int *rootY, int *winX, int *winY,
                            int *winW, int *winH, int *button1Down) {
    Window rootRet, childRet;
    int rx, ry, wx, wy;
    unsigned int mask;
    if (!XQueryPointer(d, w, &rootRet, &childRet, &rx, &ry, &wx, &wy, &mask)) {
        return 0; // pointer is on a different screen
    }
    XWindowAttributes attrs;
    if (!XGetWindowAttributes(d, w, &attrs)) {
        return 0;
    }
    *rootX = rx; *rootY = ry; *winX = wx; *winY = wy;
    *winW = attrs.width; *winH = attrs.height;
    *button1Down = (mask & Button1Mask) ? 1 : 0;
    return 1;
}

// Finds the window under (rootX, rootY) that offers XDND: descends via
// repeated XQueryPointer (each call's returned child is one level deeper)
// until reaching the deepest window at that point, then walks back UP via
// XQueryTree checking each ancestor for the XdndAware property — handles
// both "the deepest window IS the XDND target" and "XdndAware is set on
// an ancestor of it" (common with reparenting window managers).
static Window kb_find_xdnd_target(Display *d, Window root, Atom xdndAwareAtom, int rootX, int rootY) {
    Window w = root;
    for (int i = 0; i < 16; i++) {
        Window rootRet, childRet;
        int rxr, ryr, wxr, wyr;
        unsigned int mask;
        if (!XQueryPointer(d, w, &rootRet, &childRet, &rxr, &ryr, &wxr, &wyr, &mask)) {
            return None;
        }
        if (childRet == None) {
            break;
        }
        w = childRet;
    }

    Window check = w;
    for (int i = 0; i < 16 && check != None && check != root; i++) {
        Atom actualType;
        int actualFormat;
        unsigned long nitems, bytesAfter;
        unsigned char *prop = NULL;
        int status = XGetWindowProperty(d, check, xdndAwareAtom, 0, 1, False, AnyPropertyType,
                                         &actualType, &actualFormat, &nitems, &bytesAfter, &prop);
        int hasIt = (status == Success && prop != NULL && nitems >= 1);
        if (prop != NULL) {
            XFree(prop);
        }
        if (hasIt) {
            return check;
        }
        Window rootRet2, parent;
        Window *children = NULL;
        unsigned int nchildren = 0;
        if (!XQueryTree(d, check, &rootRet2, &parent, &children, &nchildren)) {
            break;
        }
        if (children != NULL) {
            XFree(children);
        }
        check = parent;
    }
    return None;
}

static long kb_xdnd_aware_version(Display *d, Window w, Atom xdndAwareAtom) {
    Atom actualType;
    int actualFormat;
    unsigned long nitems, bytesAfter;
    unsigned char *prop = NULL;
    long version = 0;
    if (XGetWindowProperty(d, w, xdndAwareAtom, 0, 1, False, AnyPropertyType,
                            &actualType, &actualFormat, &nitems, &bytesAfter, &prop) == Success) {
        if (prop != NULL) {
            if (nitems >= 1 && actualFormat == 32) {
                version = *(long *)prop;
            }
            XFree(prop);
        }
    }
    return version;
}

// data.l[0] (source window) is always ours, set here rather than passed
// from Go each time, since every XDND ClientMessage type shares that.
static void kb_send_client_message(Display *d, Window target, Window source, Atom messageType,
                                    long l1, long l2, long l3, long l4) {
    XEvent e;
    memset(&e, 0, sizeof(e));
    e.xclient.type = ClientMessage;
    e.xclient.window = target;
    e.xclient.message_type = messageType;
    e.xclient.format = 32;
    e.xclient.data.l[0] = (long)source;
    e.xclient.data.l[1] = l1;
    e.xclient.data.l[2] = l2;
    e.xclient.data.l[3] = l3;
    e.xclient.data.l[4] = l4;
    XSendEvent(d, target, False, NoEventMask, &e);
}

static void kb_send_selection_notify(Display *d, Window requestor, Atom selection, Atom target,
                                      Atom property, Time t) {
    XEvent e;
    memset(&e, 0, sizeof(e));
    e.xselection.type = SelectionNotify;
    e.xselection.requestor = requestor;
    e.xselection.selection = selection;
    e.xselection.target = target;
    e.xselection.property = property;
    e.xselection.time = t;
    XSendEvent(d, requestor, False, NoEventMask, &e);
}

// Drains any already-queued event immediately; otherwise waits up to
// timeoutMs via select() on the connection's own file descriptor. Returns
// 1 with *out populated, or 0 on timeout.
static int kb_wait_next_event(Display *d, int timeoutMs, XEvent *out) {
    if (XPending(d) > 0) {
        XNextEvent(d, out);
        return 1;
    }
    int fd = ConnectionNumber(d);
    fd_set fds;
    FD_ZERO(&fds);
    FD_SET(fd, &fds);
    struct timeval tv;
    if (timeoutMs < 0) {
        timeoutMs = 0;
    }
    tv.tv_sec = timeoutMs / 1000;
    tv.tv_usec = (timeoutMs % 1000) * 1000;
    int r = select(fd + 1, &fds, NULL, NULL, &tv);
    if (r <= 0) {
        return 0;
    }
    XNextEvent(d, out);
    return 1;
}

static int kb_event_type(XEvent *e) { return e->type; }
static Atom kb_client_message_type(XEvent *e) { return e->xclient.message_type; }
static long kb_client_message_l1(XEvent *e) { return e->xclient.data.l[1]; }
static Window kb_selection_request_requestor(XEvent *e) { return e->xselectionrequest.requestor; }
static Atom kb_selection_request_selection(XEvent *e) { return e->xselectionrequest.selection; }
static Atom kb_selection_request_target(XEvent *e) { return e->xselectionrequest.target; }
static Atom kb_selection_request_property(XEvent *e) { return e->xselectionrequest.property; }
static Time kb_selection_request_time(XEvent *e) { return e->xselectionrequest.time; }
*/
import "C"

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unsafe"
)

var (
	errNotNativeWindow   = errors.New("dragout: window does not implement driver.NativeWindow")
	errUnexpectedContext = errors.New("dragout: unexpected native context type")
)

// dragThreshold matches the small-pixel-movement convention used by the
// macOS/Windows implementations (X11 has no standard system-metric
// equivalent of SM_CXDRAG that's simple to query, so this is just a fixed,
// reasonable constant).
const dragThreshold = 4

func dragThresholdSquared() int32 { return dragThreshold * dragThreshold }

var (
	display *C.Display

	atomXdndAware      C.Atom
	atomXdndEnter      C.Atom
	atomXdndPosition   C.Atom
	atomXdndStatus     C.Atom
	atomXdndLeave      C.Atom
	atomXdndDrop       C.Atom
	atomXdndFinished   C.Atom
	atomXdndSelection  C.Atom
	atomXdndActionCopy C.Atom
	atomTextUriList    C.Atom
)

func atomName(a C.Atom) string {
	if a == C.None {
		return "None"
	}
	cName := C.XGetAtomName(display, a)
	if cName == nil {
		return fmt.Sprintf("atom#%d", uint64(a))
	}
	defer C.XFree(unsafe.Pointer(cName))
	return C.GoString(cName)
}

func internAtom(name string) C.Atom {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	return C.XInternAtom(display, cName, C.False)
}

func xdndOpenDisplay() error {
	if display != nil {
		return nil // already installed — this app only has one main window
	}
	d := C.XOpenDisplay(nil)
	if d == nil {
		return errors.New("dragout: XOpenDisplay failed (no X server?)")
	}
	display = d

	C.kb_install_error_handler()

	atomXdndAware = internAtom("XdndAware")
	atomXdndEnter = internAtom("XdndEnter")
	atomXdndPosition = internAtom("XdndPosition")
	atomXdndStatus = internAtom("XdndStatus")
	atomXdndLeave = internAtom("XdndLeave")
	atomXdndDrop = internAtom("XdndDrop")
	atomXdndFinished = internAtom("XdndFinished")
	atomXdndSelection = internAtom("XdndSelection")
	atomXdndActionCopy = internAtom("XdndActionCopy")
	atomTextUriList = internAtom("text/uri-list")
	return nil
}

// xdndQueryPointer is dragout_linux.go's idle poller's only X11 call: the
// current pointer state, with no event selection or grab involved at all
// (see dragout_linux.go's package doc for why).
func xdndQueryPointer(win uintptr) (rootX, rootY, winX, winY, winW, winH int32, button1Down, ok bool) {
	var cRootX, cRootY, cWinX, cWinY, cWinW, cWinH, cButton1 C.int
	r := C.kb_query_pointer(display, C.Window(win), &cRootX, &cRootY, &cWinX, &cWinY, &cWinW, &cWinH, &cButton1)
	if r == 0 {
		return 0, 0, 0, 0, 0, 0, false, false
	}
	return int32(cRootX), int32(cRootY), int32(cWinX), int32(cWinY), int32(cWinW), int32(cWinH), cButton1 != 0, true
}

// startDrag runs the whole XDND handshake for one drag gesture, blocking
// until it completes, is refused, or times out. Called from
// dragout_linux.go's idlePoll goroutine, never from Fyne's own main
// goroutine.
//
// Deliberately does NOT call XGrabPointer: GLFW itself holds an active
// grab for as long as the mouse button is down (confirmed by testing —
// every attempt failed with AlreadyGrabbed even after retrying for
// 300ms), and X11 only allows one active grab server-wide. Since a grab
// was only ever needed here to keep receiving position/button-state
// regardless of which window is under the cursor, this polls
// XQueryPointer instead — a passive read any client can do regardless of
// who holds the grab — for that part, and still receives XdndStatus/
// XdndFinished/SelectionRequest as real events, since those are delivered
// based on window ID / selection ownership, not on holding a grab.
func startDrag(win uintptr, paths []string) {
	d := display
	w := C.Window(win)
	root := C.XDefaultRootWindow(d)

	C.XSetSelectionOwner(d, atomXdndSelection, w, C.CurrentTime)
	defer C.XSetSelectionOwner(d, atomXdndSelection, C.None, C.CurrentTime)

	var (
		currentTarget  C.Window = C.None
		currentVersion C.long
		lastAccepted   bool
	)

	log.Println("dragout: starting XDND poll loop (no pointer grab)")
	deadline := time.Now().Add(2 * time.Minute) // hard cap so a stuck drag can't hang forever
	for time.Now().Before(deadline) {
		// Drain whatever protocol events are already queued (XdndStatus,
		// SelectionRequest) before checking pointer state this tick.
		for {
			var ev C.XEvent
			if C.kb_wait_next_event(d, 0, &ev) == 0 {
				break
			}
			switch C.kb_event_type(&ev) {
			case C.ClientMessage:
				msgType := C.kb_client_message_type(&ev)
				if msgType == atomXdndStatus {
					lastAccepted = C.kb_client_message_l1(&ev)&1 != 0
					log.Println("dragout: XdndStatus received, accepted =", lastAccepted)
				} else {
					log.Println("dragout: ClientMessage received, type =", atomName(msgType))
				}
			case C.SelectionRequest:
				handleSelectionRequest(d, &ev, paths)
			default:
				log.Println("dragout: other event received, type =", int(C.kb_event_type(&ev)))
			}
		}

		rootX, rootY, _, _, _, _, button1Down, ok := xdndQueryPointer(win)
		if !ok {
			log.Println("dragout: XQueryPointer failed mid-drag, giving up")
			break
		}

		if !button1Down {
			log.Printf("dragout: button released over target=%#x accepted=%v", uint64(currentTarget), lastAccepted)
			if currentTarget != C.None && lastAccepted {
				C.kb_send_client_message(d, currentTarget, w, atomXdndDrop, 0, C.long(C.CurrentTime), 0, 0)
				waitForFinished(d, paths)
			} else if currentTarget != C.None {
				C.kb_send_client_message(d, currentTarget, w, atomXdndLeave, 0, 0, 0, 0)
			}
			return
		}

		rx, ry := C.int(rootX), C.int(rootY)
		target := C.kb_find_xdnd_target(d, root, atomXdndAware, rx, ry)
		if target != currentTarget {
			if currentTarget != C.None {
				C.kb_send_client_message(d, currentTarget, w, atomXdndLeave, 0, 0, 0, 0)
			}
			currentVersion = 0
			lastAccepted = false
			if target != C.None {
				currentVersion = C.kb_xdnd_aware_version(d, target, atomXdndAware)
				if currentVersion > 5 {
					currentVersion = 5
				}
				C.kb_send_client_message(d, target, w, atomXdndEnter,
					currentVersion<<24, C.long(atomTextUriList), 0, 0)
			}
			log.Printf("dragout: target changed to window=%#x (xdnd version=%d)", uint64(target), int(currentVersion))
			currentTarget = target
		}
		if currentTarget != C.None {
			packed := (C.long(rx) << 16) | C.long(uint16(ry))
			C.kb_send_client_message(d, currentTarget, w, atomXdndPosition,
				0, packed, C.long(C.CurrentTime), C.long(atomXdndActionCopy))
		}

		time.Sleep(pollInterval)
	}
	log.Println("dragout: gave up (2-minute hard cap reached)")
}

// waitForFinished answers any further SelectionRequest (the target
// fetching the actual file list after Drop) while waiting for
// XdndFinished, bounded so an unresponsive target can't hang the app.
func waitForFinished(d *C.Display, paths []string) {
	log.Println("dragout: drop sent, waiting for XdndFinished")
	deadline := time.Now().Add(5 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			log.Println("dragout: timed out waiting for XdndFinished")
			return
		}
		var ev C.XEvent
		if C.kb_wait_next_event(d, C.int(remaining/time.Millisecond), &ev) == 0 {
			log.Println("dragout: timed out waiting for XdndFinished")
			return
		}
		switch C.kb_event_type(&ev) {
		case C.ClientMessage:
			if C.kb_client_message_type(&ev) == atomXdndFinished {
				log.Println("dragout: XdndFinished received")
				return
			}
		case C.SelectionRequest:
			handleSelectionRequest(d, &ev, paths)
		}
	}
}

// handleSelectionRequest answers a target's XConvertSelection request for
// XdndSelection/text/uri-list — the actual data-transfer step, exactly
// like serving clipboard data, just under XDND's own selection atom
// instead of PRIMARY/CLIPBOARD.
func handleSelectionRequest(d *C.Display, ev *C.XEvent, paths []string) {
	requestor := C.kb_selection_request_requestor(ev)
	selection := C.kb_selection_request_selection(ev)
	target := C.kb_selection_request_target(ev)
	property := C.kb_selection_request_property(ev)
	t := C.kb_selection_request_time(ev)

	log.Printf("dragout: SelectionRequest from requestor=%#x selection=%s target=%s property=%s",
		uint64(requestor), atomName(selection), atomName(target), atomName(property))

	if selection != atomXdndSelection || target != atomTextUriList || property == C.None {
		log.Println("dragout: refusing SelectionRequest (not our XdndSelection/text-uri-list)")
		C.kb_send_selection_notify(d, requestor, selection, target, C.None, t)
		return
	}

	var buf strings.Builder
	for _, p := range paths {
		buf.WriteString("file://")
		buf.WriteString(uriEncodePath(p))
		buf.WriteString("\r\n")
	}
	data := []byte(buf.String())
	if len(data) == 0 {
		C.kb_send_selection_notify(d, requestor, selection, target, C.None, t)
		return
	}

	C.XChangeProperty(d, requestor, property, target, 8, C.PropModeReplace,
		(*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)))
	C.kb_send_selection_notify(d, requestor, selection, target, property, t)
}

// uriEncodePath percent-encodes everything outside RFC 3986's unreserved
// set, byte by byte — Linux filenames are arbitrary byte sequences, not
// guaranteed valid UTF-8, so encoding by byte (rather than by rune) is the
// only encoding that's correct for every possible filename.
func uriEncodePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '/' || c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
