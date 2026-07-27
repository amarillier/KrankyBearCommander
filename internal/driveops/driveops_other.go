//go:build !darwin && !windows

package driveops

import "errors"

// Supported reports whether this platform has real Eject/OpenFormatTool
// implementations — false everywhere except macOS/Windows for now (see
// the package doc for why Linux wasn't pursued).
func Supported() bool { return false }

func IsBootVolume(string) bool { return true } // never offer Eject if it can't work anyway

func Eject(string) error { return errors.New("eject is not supported on this platform yet") }

func OpenFormatTool() error {
	return errors.New("opening a disk-management tool is not supported on this platform yet")
}
