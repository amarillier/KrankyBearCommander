//go:build windows

package fsops

import (
	"golang.org/x/sys/windows"
)

const (
	fileAttributeReadonly = 0x1
	fileAttributeHidden   = 0x2
	fileAttributeSystem   = 0x4
	fileAttributeArchive  = 0x20
)

// setAttr flips one FILE_ATTRIBUTE_* bit on path via GetFileAttributesW/
// SetFileAttributesW — the same golang.org/x/sys/windows package already
// used in internal/driveops/driveops_windows.go for its own Win32 calls.
func setAttr(path string, bit uint32, on bool) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if on {
		attrs |= bit
	} else {
		attrs &^= bit
	}
	return windows.SetFileAttributes(p, attrs)
}

func setHidden(path string, on bool) error  { return setAttr(path, fileAttributeHidden, on) }
func setArchive(path string, on bool) error { return setAttr(path, fileAttributeArchive, on) }
func setSystem(path string, on bool) error  { return setAttr(path, fileAttributeSystem, on) }
