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
	if ret != 0 {
		return fmt.Errorf("fsops: SHFileOperationW failed with code %d", ret)
	}
	return nil
}
