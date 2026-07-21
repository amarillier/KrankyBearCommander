//go:build darwin

package fsops

import (
	"fmt"
	"os"
	"path/filepath"
)

// trashPlatform moves path into the user's ~/.Trash. This is a plain rename
// into the per-user trash directory (what most non-Finder trash CLIs do),
// not a Cocoa NSWorkspace trash-item call, so it won't carry the "restore to
// original location" metadata Finder itself attaches — acceptable for Phase 1.
func trashPlatform(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("fsops: resolve home dir for trash: %w", err)
	}
	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return fmt.Errorf("fsops: create ~/.Trash: %w", err)
	}
	return os.Rename(path, uniqueDest(trashDir, filepath.Base(path)))
}
