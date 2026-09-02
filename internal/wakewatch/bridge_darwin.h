#ifndef KRANKYBEAR_WAKEWATCH_BRIDGE_DARWIN_H
#define KRANKYBEAR_WAKEWATCH_BRIDGE_DARWIN_H

// Registers an observer on NSWorkspace's notification center for
// NSWorkspaceDidWakeNotification, calling back into Go (wakewatchOnWake,
// exported from wakewatch_darwin.go) every time this happens. Only one
// observer is ever installed per process — a second call is a no-op.
void wakewatch_install(void);

#endif
