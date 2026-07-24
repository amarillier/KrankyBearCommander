//go:build !darwin && !windows && !linux

package osclipboard

import "errors"

// CopyFiles is unsupported on this platform — KrankyBear Commander only
// ships for macOS, Windows, and Linux, but this keeps the package buildable
// (rather than breaking the build) if it's ever compiled elsewhere.
func CopyFiles([]string) error {
	return errors.New("native file clipboard is not supported on this platform")
}

// PasteFiles reports nothing to paste on this platform.
func PasteFiles() ([]string, error) {
	return nil, nil
}
