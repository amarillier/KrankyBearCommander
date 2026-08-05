//go:build !windows

package main

import "errors"

// launchInteractiveShellWindows is never actually reached on this platform
// — launchInteractiveShell only calls it inside its "windows" case — but
// the symbol still needs to exist here too, matching launch_unix.go's
// showPropertiesWindows stub for the same reason.
func launchInteractiveShellWindows(shellCmd, cwd string) error {
	return errors.New("not supported on this platform")
}
