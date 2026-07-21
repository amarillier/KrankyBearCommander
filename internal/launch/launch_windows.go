//go:build windows

package launch

import "syscall"

// detachedProcess is Win32's DETACHED_PROCESS creation flag (0x00000008) —
// the child gets no console of its own, so it doesn't inherit ours. Go's
// syscall package names CREATE_NEW_PROCESS_GROUP but not this one.
const detachedProcess = 0x00000008

// detachAttr gives the child its own process group (so Ctrl+Break aimed at
// our console doesn't reach it) and no console, so it survives us exiting
// and doesn't inherit our console window.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
