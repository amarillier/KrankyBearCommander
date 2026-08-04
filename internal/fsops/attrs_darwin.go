//go:build darwin

package fsops

import "syscall"

// ufHidden is BSD/Darwin's UF_HIDDEN flag (<sys/stat.h>) — "hint that this
// item should not be displayed in a GUI." Go's syscall package doesn't
// export this constant, only the Chflags call itself, so it's hardcoded
// here (it's a stable, public part of Darwin's ABI).
const ufHidden = 0x8000

func setHidden(path string, on bool) error {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return err
	}
	flags := st.Flags
	if on {
		flags |= ufHidden
	} else {
		flags &^= ufHidden
	}
	return syscall.Chflags(path, int(flags))
}

// Archive/System have no Darwin equivalent — the Change Attributes UI only
// offers them on Windows, so these are never actually invoked with an
// on/off request in practice, but still need to exist for the package to
// build on this platform.
func setArchive(string, bool) error { return nil }
func setSystem(string, bool) error  { return nil }
