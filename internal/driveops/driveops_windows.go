package driveops

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

// Direct Win32 syscalls for Format's elevation prompt, not a PowerShell
// subprocess — same reasoning as osclipboard_windows.go: PowerShell is a
// "living off the land" pattern security tooling commonly flags, and
// shell32 is an ordinary system DLL virtually every native Windows
// application already links against. Eject uses go-ole (see below) rather
// than raw syscalls, since it needs COM automation, not a plain DLL call.
var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// Supported reports whether this platform has real Eject/OpenFormatTool
// implementations — true on Windows.
func Supported() bool { return true }

// IsBootVolume reports whether path is the system drive (e.g. "C:\"),
// which should never be offered an Eject option.
func IsBootVolume(path string) bool {
	sysDrive := os.Getenv("SystemDrive") // e.g. "C:", no trailing backslash
	if sysDrive == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(path, `\`), sysDrive)
}

// csidlDrives is CSIDL_DRIVES ("My Computer"), the argument Shell.
// Application's NameSpace() takes to reach the drives/volumes view.
const csidlDrives = 17

// oleAlreadyInitialized is S_FALSE, what CoInitialize returns (as an
// error, since its HRESULT is nonzero) when COM is already initialized on
// this thread — e.g. by internal/dragout at startup. Not a real failure.
const oleAlreadyInitialized = 1

// Eject invokes the "Eject" verb via the Shell.Application COM automation
// object — the exact mechanism Explorer's own right-click Eject uses, via
// github.com/go-ole/go-ole (already an indirect dependency via Fyne
// itself) rather than hand-rolled IDispatch marshaling: VARIANT/DISPPARAMS
// marshaling is fiddly enough to get exactly right that a mature, widely
// used library is the safer choice here, especially blind (no Windows
// machine to test against locally).
//
// Deliberately NOT the more obvious CreateFile + FSCTL_LOCK_VOLUME /
// FSCTL_DISMOUNT_VOLUME / IOCTL_STORAGE_EJECT_MEDIA route: real-world
// testing showed that needs administrator privileges even for an ordinary
// user ejecting their own USB drive (unlike Explorer's own Eject, which
// doesn't), and prompting UAC on every eject would be a worse experience
// than TotalCmd/Explorer's own.
func Eject(path string) error {
	if err := ole.CoInitialize(0); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) || oleErr.Code() != oleAlreadyInitialized {
			return fmt.Errorf("CoInitialize failed: %w", err)
		}
	}
	defer ole.CoUninitialize()

	shellUnknown, err := oleutil.CreateObject("Shell.Application")
	if err != nil {
		return fmt.Errorf("could not create Shell.Application: %w", err)
	}
	defer shellUnknown.Release()

	shellDisp, err := shellUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("could not query IDispatch: %w", err)
	}
	defer shellDisp.Release()

	nsResult, err := shellDisp.CallMethod("NameSpace", csidlDrives)
	if err != nil {
		return fmt.Errorf("NameSpace failed: %w", err)
	}
	nsDisp := nsResult.ToIDispatch()
	if nsDisp == nil {
		return errors.New("NameSpace(drives) returned nothing")
	}
	defer nsDisp.Release()

	itemResult, err := nsDisp.CallMethod("ParseName", path)
	if err != nil {
		return fmt.Errorf("ParseName(%s) failed: %w", path, err)
	}
	itemDisp := itemResult.ToIDispatch()
	if itemDisp == nil {
		return fmt.Errorf("drive %s not found", path)
	}
	defer itemDisp.Release()

	if _, err := itemDisp.CallMethod("InvokeVerb", "Eject"); err != nil {
		return fmt.Errorf("eject failed: %w", err)
	}
	return nil
}

const swShowNormal = 1

// OpenFormatTool launches the Disk Management snap-in — see the package
// doc for why this doesn't try to auto-select a specific drive. mmc.exe is
// the ordinary Microsoft Management Console host, not a scripting engine.
//
// Disk Management requires administrator privileges to open at all, which
// a plain CreateProcess (what os/exec uses) can't request — it just fails
// with "requires elevation". ShellExecuteW's "runas" verb is the
// documented, correct way to ask for that: it pops the normal Windows UAC
// consent prompt, then launches elevated if approved, exactly like
// double-clicking a shortcut marked "Run as administrator" would.
func OpenFormatTool() error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString("mmc.exe")
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString("diskmgmt.msc")
	if err != nil {
		return err
	}

	ret, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		0,
		swShowNormal,
	)
	// ShellExecuteW returns a value > 32 on success; anything <= 32 is an
	// error code (per its documented HINSTANCE-as-status-code contract).
	if ret <= 32 {
		if ret == 1223 { // ERROR_CANCELLED
			return errors.New("elevation was cancelled")
		}
		return fmt.Errorf("could not open Disk Management: %w", callErr)
	}
	return nil
}
