//go:build !windows && !darwin

package fsops

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trashPlatform implements the freedesktop.org Trash spec's "home trash"
// (files/ + info/ under $XDG_DATA_HOME/Trash), which every major Linux
// desktop (GNOME, KDE, XFCE, ...) reads to populate its own Trash view.
// Cross-filesystem items (rename fails with EXDEV) fall back to copy+remove
// rather than implementing the spec's per-mountpoint $topdir/.Trash-$uid,
// which is enough for the common case of trashing within the home volume.
func trashPlatform(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("fsops: resolve absolute path for trash: %w", err)
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("fsops: resolve home dir for trash: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	trashDir := filepath.Join(dataHome, "Trash")
	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return fmt.Errorf("fsops: create Trash/files: %w", err)
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return fmt.Errorf("fsops: create Trash/info: %w", err)
	}

	name := filepath.Base(abs)
	dest := uniqueDest(filesDir, name)
	trashedName := filepath.Base(dest)
	infoPath := filepath.Join(infoDir, trashedName+".trashinfo")

	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		encodeTrashPath(abs), time.Now().Format("2006-01-02T15:04:05"))
	if err := os.WriteFile(infoPath, []byte(info), 0o600); err != nil {
		return fmt.Errorf("fsops: write .trashinfo: %w", err)
	}

	if err := os.Rename(abs, dest); err == nil {
		return nil
	}

	// Cross-device: copy then remove the original.
	total, err := totalSize([]string{abs})
	if err != nil {
		os.Remove(infoPath)
		return err
	}
	var done int64
	if err := copyPath(abs, dest, &done, total, noProgress, noConflict); err != nil {
		os.Remove(infoPath)
		return err
	}
	return os.RemoveAll(abs)
}

// encodeTrashPath percent-encodes each path segment per the Trash spec,
// leaving the "/" separators intact.
func encodeTrashPath(absPath string) string {
	segments := strings.Split(absPath, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}
