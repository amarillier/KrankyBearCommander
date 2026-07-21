// Package fsops implements Copy/Move/Delete/Rename/Mkdir over the local
// filesystem for the F5/F6/F7/F8 file operations. It has no Fyne dependency:
// callers (fileops_ui.go) supply plain closures for progress reporting and
// conflict resolution, and the package is unit-tested directly with
// t.TempDir(). Trash support is platform-specific (see trash_*.go).
package fsops

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ConflictAction is the caller's decision when a destination path already
// exists.
type ConflictAction int

const (
	ConflictOverwrite ConflictAction = iota
	ConflictSkip
	ConflictRename
	ConflictCancel
)

// ProgressFunc reports cumulative bytes copied/moved so far against the
// best-effort total, and the path currently being processed. A whole-item
// rename-based move (same filesystem, no byte copy) reports done=total=0 for
// that item since there is nothing to measure incrementally.
type ProgressFunc func(done, total int64, currentPath string)

// ConflictFunc is invoked with the destination path that already exists.
// newName is only consulted when action == ConflictRename, and must be a
// name that does not itself already exist in the destination directory —
// Copy/Move do not loop to re-check it.
type ConflictFunc func(destPath string) (action ConflictAction, newName string)

// ErrCancelled is returned when a ConflictFunc returns ConflictCancel.
var ErrCancelled = errors.New("operation cancelled")

func noProgress(int64, int64, string)            {}
func noConflict(string) (ConflictAction, string) { return ConflictOverwrite, "" }

// Copy recursively copies each of sources into destDir, preserving each
// source's base name. Conflicts are resolved per file (directories are
// merged, not replaced wholesale).
func Copy(sources []string, destDir string, progress ProgressFunc, resolve ConflictFunc) error {
	if progress == nil {
		progress = noProgress
	}
	if resolve == nil {
		resolve = noConflict
	}

	total, err := totalSize(sources)
	if err != nil {
		return err
	}

	var done int64
	for _, src := range sources {
		dest := filepath.Join(destDir, filepath.Base(src))
		if err := copyPath(src, dest, &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

// Move relocates each of sources into destDir. It tries an atomic rename
// first (fast path, same filesystem); on any failure (typically a cross-
// device rename) it falls back to copying then removing the source.
// Conflict resolution happens once per top-level source item.
func Move(sources []string, destDir string, progress ProgressFunc, resolve ConflictFunc) error {
	if progress == nil {
		progress = noProgress
	}
	if resolve == nil {
		resolve = noConflict
	}

	for _, src := range sources {
		dest := filepath.Join(destDir, filepath.Base(src))

		if _, err := os.Lstat(dest); err == nil {
			action, newName := resolve(dest)
			switch action {
			case ConflictSkip:
				continue
			case ConflictRename:
				dest = filepath.Join(destDir, newName)
			case ConflictCancel:
				return ErrCancelled
			case ConflictOverwrite:
				if err := os.RemoveAll(dest); err != nil {
					return err
				}
			}
		}

		if err := os.Rename(src, dest); err == nil {
			progress(0, 0, src)
			continue
		}

		total, err := totalSize([]string{src})
		if err != nil {
			return err
		}
		var done int64
		if err := copyPath(src, dest, &done, total, progress, noConflict); err != nil {
			return err
		}
		if err := os.RemoveAll(src); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes each path. permanent=false sends to the OS trash (see
// trash_*.go); permanent=true (Shift+F8) removes immediately and
// unrecoverably.
func Delete(paths []string, permanent bool) error {
	for _, p := range paths {
		if permanent {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			continue
		}
		if err := trashPlatform(p); err != nil {
			return err
		}
	}
	return nil
}

// Rename renames oldPath to newPath (F6 rename-in-place).
func Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// Mkdir creates path, including any missing intermediate directories (F7;
// lets a user type "new/sub" in one go).
func Mkdir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func totalSize(paths []string) (int64, error) {
	var total int64
	for _, p := range paths {
		err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				info, err := d.Info()
				if err != nil {
					return err
				}
				total += info.Size()
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

func copyPath(src, dest string, done *int64, total int64, progress ProgressFunc, resolve ConflictFunc) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dest, done, total, progress, resolve)
	}
	return copyFile(src, dest, info, done, total, progress, resolve)
}

func copyDir(src, dest string, done *int64, total int64, progress ProgressFunc, resolve ConflictFunc) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dest, e.Name()), done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dest string, info os.FileInfo, done *int64, total int64, progress ProgressFunc, resolve ConflictFunc) error {
	if _, err := os.Lstat(dest); err == nil {
		action, newName := resolve(dest)
		switch action {
		case ConflictSkip:
			*done += info.Size()
			progress(*done, total, src)
			return nil
		case ConflictCancel:
			return ErrCancelled
		case ConflictRename:
			dest = filepath.Join(filepath.Dir(dest), newName)
		case ConflictOverwrite:
			// proceed, OpenFile below truncates it.
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 256*1024)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			*done += int64(n)
			progress(*done, total, src)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return out.Close()
}
