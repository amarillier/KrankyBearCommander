//go:build windows

package fsops

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

// shFileOpStruct mirrors the Win32 SHFILEOPSTRUCTW layout used to invoke the
// Recycle Bin via shell32.SHFileOperationW.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x0003
	fofAllowUndo      = 0x0040 // send to Recycle Bin instead of deleting outright
	fofNoConfirmation = 0x0010
	fofSilent         = 0x0004
)

var (
	modShell32           = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperationW = modShell32.NewProc("SHFileOperationW")
)

// errFileNotFound is ERROR_FILE_NOT_FOUND — SHFileOperationW falls through
// to plain Win32 error codes (rather than one of its own DE_* pseudo-codes)
// when the underlying file API it calls fails, and this is by far the most
// common case in practice: the selection went stale (something else already
// removed the file — a real repro was a background Move's own cleanup pass
// racing a F8 Delete on the same, now-already-gone item). The desired end
// state — this path being gone — already holds, so treat it as success
// rather than an error a user has to click through.
const errFileNotFound = 2

// trashPlatform sends path to the Windows Recycle Bin via SHFileOperationW.
func trashPlatform(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("fsops: resolve absolute path for trash: %w", err)
	}

	// pFrom is a list of null-terminated strings, itself terminated by an
	// extra null — a single item still needs that trailing double-null.
	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return fmt.Errorf("fsops: encode path for trash: %w", err)
	}
	from = append(from, 0)

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent,
	}
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if ret == errFileNotFound {
		return nil
	}
	if ret != 0 {
		return fmt.Errorf("couldn't send %q to the Recycle Bin (Windows error code %d)", filepath.Base(abs), ret)
	}
	return nil
}
