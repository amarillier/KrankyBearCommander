//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// launchInteractiveShellWindows opens a real, visible console window
// running shellCmd (cmd/powershell/pwsh, as typed), rooted at cwd —
// deliberately CREATE_NEW_CONSOLE, the opposite of internal/launch's
// detachAttr (DETACHED_PROCESS, see launch_windows.go), since an
// interactive shell needs a console to be usable at all.
func launchInteractiveShellWindows(shellCmd, cwd string) error {
	cmd := exec.Command(shellCmd)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	return cmd.Start()
}
