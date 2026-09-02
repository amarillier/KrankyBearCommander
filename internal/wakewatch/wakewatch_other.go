//go:build !darwin

package wakewatch

// Supported reports whether this platform can detect OS wake-from-sleep —
// false everywhere except macOS for now.
func Supported() bool { return false }

// Install is a no-op on platforms without a wake-notification implementation.
func Install(func()) {}
