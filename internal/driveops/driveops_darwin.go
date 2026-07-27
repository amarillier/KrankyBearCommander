package driveops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Supported reports whether this platform has real Eject/OpenFormatTool
// implementations — true on macOS.
func Supported() bool { return true }

// IsBootVolume reports whether path is the boot volume, which should never
// be offered an Eject option. "/" obviously is — but modern macOS's
// separate System/Data volume architecture also surfaces the boot volume
// as its own named entry under /Volumes (typically "Macintosh HD", though
// it can be renamed), which "/" alone wouldn't catch: found the hard way
// when a real test attempted to eject exactly that entry (macOS itself
// refused, since it's in use, but it shouldn't have been offered at all).
// Comparing device IDs catches it regardless of what it's named.
func IsBootVolume(path string) bool {
	if path == "/" {
		return true
	}
	root, err := os.Stat("/")
	if err != nil {
		return false
	}
	candidate, err := os.Stat(path)
	if err != nil {
		return false
	}
	rootStat, ok1 := root.Sys().(*syscall.Stat_t)
	candStat, ok2 := candidate.Sys().(*syscall.Stat_t)
	if !ok1 || !ok2 {
		return false
	}
	return rootStat.Dev == candStat.Dev
}

// Eject unmounts and (for physically removable media) ejects the volume
// at path — diskutil is an ordinary system utility, not a scripting/
// automation engine, so this doesn't run afoul of the same osascript/
// PowerShell concern as elsewhere in this app.
func Eject(path string) error {
	out, err := exec.Command("diskutil", "eject", path).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("eject failed: %s", msg)
	}
	return nil
}

// OpenFormatTool launches Disk Utility — see the package doc for why this
// doesn't try to auto-select a specific volume.
func OpenFormatTool() error {
	return exec.Command("open", "-a", "Disk Utility").Start()
}
