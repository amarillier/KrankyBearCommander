//go:build windows

package fsops

import (
	"errors"
	"syscall"
)

// ERROR_NOT_READY and ERROR_WRONG_DISK are the two Win32 codes observed
// reading from a removable drive/card reader that hasn't finished settling
// right after insertion (real-world repro: fresh SD card, Move fails on the
// very first file with a ~450ms-slow open followed by an instant read
// failure; a retry a minute or so later succeeds with no code change at
// all) — see IsTransientRemovableMediaError.
const (
	errWinNotReady  = 21
	errWinWrongDisk = 34
)

// IsTransientRemovableMediaError reports whether err is one of those two
// codes. Windows' own FormatMessage text for them ("the wrong diskette is
// in the drive", with unresolved "%1/%2/%3" placeholders — a Go
// syscall.Errno.Error() quirk, it doesn't supply FormatMessage's insertion
// strings) is confusing and has nothing to do with the actual source or
// destination, so the UI swaps in a clearer message when it sees this.
func IsTransientRemovableMediaError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch int(errno) {
	case errWinNotReady, errWinWrongDisk:
		return true
	default:
		return false
	}
}
