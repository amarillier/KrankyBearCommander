package osclipboard

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 clipboard/DROPFILES support, called directly via syscalls rather
// than a PowerShell subprocess (Set-Clipboard/Get-Clipboard can do this,
// but spawning powershell.exe is a "living off the land" pattern security
// tooling commonly flags — see this package's doc comment). user32/
// kernel32/shell32 are the same ordinary system DLLs virtually every native
// Windows application already links against.
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")

	procDragQueryFileW = shell32.NewProc("DragQueryFileW")
)

const (
	cfHDROP      = 15
	gmemMoveable = 0x0002
)

// dropFiles mirrors the Win32 DROPFILES header (see <shlobj.h>) prepended
// to a CF_HDROP clipboard payload: DWORD pFiles; POINT pt; BOOL fNC, fWide.
type dropFiles struct {
	pFiles uint32
	pt     struct{ x, y int32 }
	fNC    int32
	fWide  int32
}

// CopyFiles puts paths on the clipboard in CF_HDROP format, the same
// format Explorer's own Copy places there — Explorer's Paste then copies
// them as real files.
func CopyFiles(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no files to copy")
	}

	var buf []uint16
	for _, p := range paths {
		u, err := windows.UTF16FromString(p)
		if err != nil {
			return err
		}
		buf = append(buf, u[:len(u)-1]...) // drop its own nul; separators added below
		buf = append(buf, 0)
	}
	buf = append(buf, 0) // CF_HDROP's file list is double-nul-terminated

	headerSize := int(unsafe.Sizeof(dropFiles{}))
	dataSize := uintptr(headerSize + len(buf)*2)

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, dataSize)
	if hMem == 0 {
		return errors.New("GlobalAlloc failed")
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return errors.New("GlobalLock failed")
	}

	// `go vet` flags both of the next two conversions (unsafeptr: uintptr ->
	// unsafe.Pointer, one after arithmetic) as potentially unsafe — that
	// check exists for uintptrs aliasing Go's GC-managed heap, where the
	// object could move between conversions. ptr is neither: GlobalLock
	// returns a raw OS memory address entirely outside Go's heap, so there
	// is nothing for the GC to move out from under it. Expected, safe to
	// disregard for this file specifically.
	header := (*dropFiles)(unsafe.Pointer(ptr))
	*header = dropFiles{pFiles: uint32(headerSize), fWide: 1}

	dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr+uintptr(headerSize))), len(buf))
	copy(dst, buf)
	procGlobalUnlock.Call(hMem)

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return errors.New("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()
	// Ownership of hMem transfers to the OS on success — it must NOT be
	// freed here; the system releases it once the clipboard changes again.
	if h, _, _ := procSetClipboardData.Call(cfHDROP, hMem); h == 0 {
		return errors.New("SetClipboardData failed")
	}
	return nil
}

// PasteFiles reads file paths from the clipboard's CF_HDROP data (e.g.
// after Ctrl+C on one or more items in Explorer), or nil if it doesn't
// currently hold any.
func PasteFiles() ([]string, error) {
	if avail, _, _ := procIsClipboardFormatAvailable.Call(cfHDROP); avail == 0 {
		return nil, nil
	}

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return nil, errors.New("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()

	hDrop, _, _ := procGetClipboardData.Call(cfHDROP)
	if hDrop == 0 {
		return nil, nil
	}

	const queryCount = 0xFFFFFFFF // DragQueryFileW's "how many files" sentinel index
	count, _, _ := procDragQueryFileW.Call(hDrop, queryCount, 0, 0)

	paths := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		size, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
		nameBuf := make([]uint16, size+1)
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&nameBuf[0])), size+1)
		paths = append(paths, windows.UTF16ToString(nameBuf))
	}
	return paths, nil
}
