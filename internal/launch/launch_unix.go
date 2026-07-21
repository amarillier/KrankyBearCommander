//go:build !windows

package launch

import "syscall"

// detachAttr makes the child its own session leader, detaching it from our
// controlling terminal and process group so it doesn't receive signals
// (e.g. SIGHUP/SIGINT) meant for us and keeps running after we exit.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
