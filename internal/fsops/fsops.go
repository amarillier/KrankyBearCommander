// Package fsops implements Copy/Move/Delete/Rename/Mkdir over the local
// filesystem for the F5/F6/F7/F8 file operations. It has no Fyne dependency:
// callers (fileops_ui.go) supply plain closures for progress reporting and
// conflict resolution, and the package is unit-tested directly with
// t.TempDir(). Trash support is platform-specific (see trash_*.go).
package fsops

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// Duplicate copies path to a sibling in the same directory named "<name>
// copy", "<name> copy 2", etc. (right-click "Duplicate") — unlike Copy, which
// always targets the *other* pane, this clones an item in place, so it picks
// its own non-colliding destination name rather than going through the
// conflict-resolution dialog.
func Duplicate(path string) (string, error) {
	dest := duplicateName(filepath.Dir(path), filepath.Base(path))
	total, err := totalSize([]string{path})
	if err != nil {
		return "", err
	}
	var done int64
	if err := copyPath(path, dest, &done, total, noProgress, noConflict); err != nil {
		return "", err
	}
	return dest, nil
}

// duplicateName returns "<dir>/<stem> copy<ext>", or "<dir>/<stem> copy
// N<ext>" for the first N>=2 that doesn't already exist in dir.
func duplicateName(dir, base string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	candidate := filepath.Join(dir, stem+" copy"+ext)
	for n := 2; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s copy %d%s", stem, n, ext))
	}
}

// Rename renames oldPath to newPath (F6 rename-in-place).
func Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// Symlink creates a symbolic link at linkPath pointing to target
// (right-click "Create Symbolic Link…").
func Symlink(target, linkPath string) error {
	return os.Symlink(target, linkPath)
}

// CompressName returns a non-colliding "<name>.<ext>" (single source) or
// "Archive.<ext>"/"Archive N.<ext>" (multiple sources) destination path in
// dir — mirrors Duplicate's own-non-colliding-name convention, since
// compressing (like duplicating) creates its output alongside the
// source(s) rather than in the other pane.
func CompressName(dir string, sources []string, ext string) string {
	base := "Archive"
	if len(sources) == 1 {
		b := filepath.Base(sources[0])
		if trimmed := strings.TrimSuffix(b, filepath.Ext(b)); trimmed != "" {
			base = trimmed
		} else {
			base = b
		}
	}
	candidate := filepath.Join(dir, base+"."+ext)
	for n := 2; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s %d.%s", base, n, ext))
	}
}

// Compress creates a zip archive at destZip containing each of sources
// (files and/or directories, recursively), using each source's base name as
// its root within the archive — the stdlib archive/zip path, always
// available with no external dependency (compare CompressSevenZip, which
// needs an actual 7z-capable binary).
func Compress(sources []string, destZip string) error {
	out, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer out.Close()

	zw := zip.NewWriter(out)
	for _, src := range sources {
		info, err := os.Lstat(src)
		if err != nil {
			zw.Close()
			return err
		}
		base := filepath.Base(src)
		if info.IsDir() {
			err = addDirToZip(zw, src, base)
		} else {
			err = addFileToZip(zw, src, base, info)
		}
		if err != nil {
			zw.Close()
			return err
		}
	}
	return zw.Close()
}

func addDirToZip(zw *zip.Writer, dir, archiveBase string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		archivePath := archiveBase
		if rel != "." {
			archivePath = filepath.ToSlash(filepath.Join(archiveBase, rel))
		}
		if d.IsDir() {
			_, err := zw.Create(archivePath + "/")
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return addFileToZip(zw, path, archivePath, info)
	})
}

func addFileToZip(zw *zip.Writer, path, archivePath string, info os.FileInfo) error {
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(archivePath)
	hdr.Method = zip.Deflate

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	_, err = io.Copy(w, in)
	return err
}

// sevenZipCandidates is the order 7z-capable binaries are looked up on PATH
// when the user hasn't configured an explicit override — 7z (the full
// p7zip/7-Zip CLI name on most systems), then the shorter names some
// packages/builds use.
var sevenZipCandidates = []string{"7z", "7za", "7zz"}

// SevenZipAvailable reports whether a 7z-capable binary is usable, and its
// path — override (a user-configured path, see commander's 7-Zip Binary
// Path… setting) takes priority when non-empty; otherwise it's a PATH
// lookup. Callers use this only to decide whether to offer "Compress to
// .7z" in the UI at all.
func SevenZipAvailable(override string) (string, bool) {
	if override != "" {
		if info, err := os.Stat(override); err == nil && !info.IsDir() {
			return override, true
		}
		return "", false
	}
	for _, name := range sevenZipCandidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

// CompressSevenZip shells out to binary (see SevenZipAvailable) to create a
// .7z archive at destArchive containing each of sources — there is no
// usable pure-Go .7z writer, unlike Compress's stdlib archive/zip path.
func CompressSevenZip(binary string, sources []string, destArchive string) error {
	args := append([]string{"a", destArchive}, sources...)
	out, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Mkdir creates path, including any missing intermediate directories (F7;
// lets a user type "new/sub" in one go).
func Mkdir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// DirSize returns the total size of every file under path, walked
// recursively (du -s-style) — used by the "Calculate Folder Sizes" command.
func DirSize(path string) (int64, error) {
	return totalSize([]string{path})
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
