//go:build !windows

package launch

import (
	"errors"
	"syscall"
)

// detachAttr makes the child its own session leader, detaching it from our
// controlling terminal and process group so it doesn't receive signals
// (e.g. SIGHUP/SIGINT) meant for us and keeps running after we exit.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// showPropertiesWindows is never actually reached on this platform —
// ShowProperties only calls it inside its "windows" case — but the symbol
// still needs to exist for launch.go (no build tag of its own) to compile
// here too.
func showPropertiesWindows(string) error {
	return errors.New("not supported on this platform")
}
