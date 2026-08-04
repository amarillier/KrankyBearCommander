//go:build windows

package launch

import (
	"syscall"

	"golang.org/x/sys/windows"
)

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

// showPropertiesWindows invokes the Win32 Shell API's "properties" verb —
// the same one Explorer's own right-click Properties uses — via
// golang.org/x/sys/windows.ShellExecute, already a direct dependency
// (internal/driveops/driveops_windows.go uses the same package for its own
// Win32 calls). No admin/elevation needed for this verb.
func showPropertiesWindows(path string) error {
	verb, err := windows.UTF16PtrFromString("properties")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
