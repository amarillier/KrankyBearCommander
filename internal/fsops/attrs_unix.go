//go:build !windows && !darwin

package fsops

// Hidden/Archive/System have no real attribute-bit equivalent on Linux/BSD
// — "hidden" there is a leading-dot filename convention, not a settable
// attribute (see attributes_ui.go, which doesn't offer any of these three
// checkboxes on this platform) — but SetAttributes still needs these to
// exist to build here.
func setHidden(string, bool) error  { return nil }
func setArchive(string, bool) error { return nil }
func setSystem(string, bool) error  { return nil }
