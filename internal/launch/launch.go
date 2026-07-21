// Package launch opens a file with the OS's default association, EXCEPT for
// files with the executable bit set (or a .exe/.bat/.cmd extension on
// Windows), which are spawned directly as detached processes instead.
//
// Two problems this avoids: (1) the OS "open" mechanism only knows document
// associations, not "run this" — on macOS, `open` on a bare executable/script
// actually launches it inside a new Terminal.app window rather than running
// it directly, which is not what a double-click on a program should do; (2)
// a plain os/exec child inherits our process group, so if we don't detach it
// explicitly, closing/signalling the file manager's process group can take
// the launched program down with it — the whole point of "launch" is that it
// keeps running after the file manager quits.
package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// IsExecutable reports whether path should be spawned directly rather than
// handed to the OS's default file association.
func IsExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd":
			return true
		default:
			return false
		}
	}
	return info.Mode().Perm()&0o111 != 0
}

// Open launches path: directly and detached if it's executable, otherwise
// via the OS's default file association.
func Open(path string) error {
	if IsExecutable(path) {
		return spawnDetached(path)
	}
	return openWithDefaultApp(path)
}

// spawnDetached runs path with detachAttr (see launch_unix.go /
// launch_windows.go) so it survives this process exiting and, on macOS,
// doesn't get wrapped in a Terminal.app window the way `open` would.
func spawnDetached(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = detachAttr()
	return cmd.Start()
}

func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
