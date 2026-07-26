#ifndef KRANKYBEAR_DRAGOUT_BRIDGE_DARWIN_H
#define KRANKYBEAR_DRAGOUT_BRIDGE_DARWIN_H

// Installs a local NSEvent monitor on the given NSWindow (passed as a raw
// pointer, matching fyne's driver.MacWindowContext.NSWindow) that starts a
// native file-drag session (NSDraggingSession) once the mouse moves past
// the OS drag threshold while the left button is held down. Calls back
// into Go (dragoutHitTest, exported from dragout_darwin.go — see
// _cgo_export.h) at the mouse-down point to decide what to drag; a NULL or
// empty result leaves the click alone and no drag starts. Only one window
// is supported (this app only ever installs this on its single main
// window) — a second call is a no-op.
void dragout_install(void *nsWindowPtr);

#endif
