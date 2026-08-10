//go:build !windows

package fsops

// IsTransientRemovableMediaError is Windows-specific (see fsops_windows.go)
// — the equivalent POSIX errno values mean unrelated things (e.g. EISDIR),
// so this is never true on other platforms.
func IsTransientRemovableMediaError(error) bool { return false }
