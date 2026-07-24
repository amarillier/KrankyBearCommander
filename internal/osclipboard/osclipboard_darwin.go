// Package osclipboard puts real files on the OS clipboard (so Finder/
// Explorer/Nautilus can Paste them as actual files, not just a name/path
// string — see contextmenu_ui.go's existing Copy Name/Copy Path for that
// simpler case) and reads them back. Deliberately NOT implemented via
// osascript (macOS) or a PowerShell subprocess (Windows): both are
// automation/scripting engines that security tooling commonly watches for,
// and would flag this app's entirely ordinary clipboard use as suspicious.
// macOS instead calls NSPasteboard directly via a small Cocoa bridge (this
// project already requires a full cgo toolchain regardless, since Fyne's
// desktop backend wraps GLFW via cgo); Windows calls the Win32 clipboard
// API directly via syscalls (see osclipboard_windows.go); Linux shells out
// to xclip/wl-copy — narrow, single-purpose clipboard utilities, not
// general automation engines (see osclipboard_linux.go).
package osclipboard

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "bridge_darwin.h"
*/
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

// CopyFiles puts paths on the general pasteboard as real file references,
// replacing its current contents.
func CopyFiles(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no files to copy")
	}
	joined := strings.Join(paths, "\x00") + "\x00"
	cJoined := C.CString(joined)
	defer C.free(unsafe.Pointer(cJoined))

	if C.clipboard_write_files(cJoined, C.int(len(paths))) != 0 {
		return errors.New("failed to write files to the pasteboard")
	}
	return nil
}

// PasteFiles reads file references from the general pasteboard, or nil if
// it doesn't currently hold any (not an error — just nothing to paste).
func PasteFiles() ([]string, error) {
	var count C.int
	cResult := C.clipboard_read_files(&count)
	if cResult == nil {
		return nil, nil
	}
	defer C.clipboard_free(cResult)

	n := int(count)
	paths := make([]string, 0, n)
	p := unsafe.Pointer(cResult)
	for i := 0; i < n; i++ {
		s := C.GoString((*C.char)(p))
		paths = append(paths, s)
		p = unsafe.Pointer(uintptr(p) + uintptr(len(s)) + 1)
	}
	return paths, nil
}
