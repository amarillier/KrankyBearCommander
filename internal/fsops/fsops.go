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
	"time"
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
// that item since there is nothing to measure incrementally. currentPath is
// sometimes a plain descriptive phase label (e.g. "Cleaning up: <name>")
// rather than a real path — callers displaying it should not assume it's a
// filesystem path.
//
// The return value reports whether the operation should continue — false
// means the caller has requested cancellation, and every consumer of a
// ProgressFunc must stop promptly (returning ErrCancelled) rather than
// pushing through to completion.
type ProgressFunc func(done, total int64, currentPath string) bool

// ConflictFunc is invoked with the destination path that already exists.
// newName is only consulted when action == ConflictRename, and must be a
// name that does not itself already exist in the destination directory —
// Copy/Move do not loop to re-check it.
type ConflictFunc func(destPath string) (action ConflictAction, newName string)

// ErrCancelled is returned when a ConflictFunc returns ConflictCancel.
var ErrCancelled = errors.New("operation cancelled")

func noProgress(int64, int64, string) bool       { return true }
func noConflict(string) (ConflictAction, string) { return ConflictOverwrite, "" }

// retryAttempts/retryDelay: a Process Monitor capture of the real
// "wrong diskette"/ERROR_WRONG_DISK failure (see IsTransientRemovableMediaError)
// showed Windows' own internal volume-verify recovery — visible as it
// re-reading the raw volume's boot sector/FAT tables — completing in
// 0.5-1.2s every time observed, after which the exact same access that just
// failed succeeds normally. 3 attempts at 700ms apart comfortably covers
// that with margin.
const (
	retryAttempts = 3
	retryDelay    = 700 * time.Millisecond
)

// retryTransient calls fn, retrying it (up to retryAttempts total) if it
// fails with a transient removable-media error, sleeping retryDelay between
// attempts. Any other error, or the last attempt's error, is returned as-is.
func retryTransient(fn func() error) error {
	var err error
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		err = fn()
		if err == nil || !IsTransientRemovableMediaError(err) {
			return err
		}
		if attempt < retryAttempts {
			time.Sleep(retryDelay)
		}
	}
	return err
}

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
// Conflict resolution happens once per top-level source item. The items
// needing the copy+delete fallback share one combined total/done across
// all of them (rather than resetting per item), so a multi-item Move shows
// one continuous progress bar instead of restarting at 0% for each item.
func Move(sources []string, destDir string, progress ProgressFunc, resolve ConflictFunc) error {
	if progress == nil {
		progress = noProgress
	}
	if resolve == nil {
		resolve = noConflict
	}

	type pendingItem struct{ src, dest string }
	var pending []pendingItem

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
			if !progress(0, 0, src) {
				return ErrCancelled
			}
			continue
		}

		pending = append(pending, pendingItem{src, dest})
	}

	if len(pending) == 0 {
		return nil
	}

	srcs := make([]string, len(pending))
	for i, p := range pending {
		srcs[i] = p.src
	}
	total, err := totalSize(srcs)
	if err != nil {
		return err
	}

	var done int64
	for _, p := range pending {
		if err := copyPath(p.src, p.dest, &done, total, progress, noConflict); err != nil {
			return err
		}
		if !progress(done, total, "Cleaning up: "+filepath.Base(p.src)) {
			return ErrCancelled
		}
		if err := removeAllWithProgress(p.src, &done, total, progress); err != nil {
			return err
		}
	}
	return nil
}

// removeAllWithProgress removes path (file or directory tree), reporting
// each entry via progress before removing it — Move's "Cleaning up" phase
// (see Move) is otherwise silent, unlike os.RemoveAll which it replaces.
func removeAllWithProgress(path string, done *int64, total int64, progress ProgressFunc) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !progress(*done, total, "Cleaning up: "+e.Name()) {
				return ErrCancelled
			}
			if err := removeAllWithProgress(filepath.Join(path, e.Name()), done, total, progress); err != nil {
				return err
			}
		}
	}
	return os.Remove(path)
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

// SymlinkName returns a non-colliding "<dir>/link-<base>", or "<dir>/link-
// <stem> N<ext>" for the first N>=2 that doesn't already exist — the
// default suggested when creating a symbolic link, alongside the source in
// the same directory.
func SymlinkName(dir, base string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	candidate := filepath.Join(dir, "link-"+stem+ext)
	for n := 2; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("link-%s %d%s", stem, n, ext))
	}
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

// AttrChange is one attribute's requested change in a batch Change
// Attributes operation (see SetAttributes) — a tri-state in place of a
// plain bool, since a batch over a mixed selection needs a way to say
// "leave however each file already has this one," not just set/clear.
type AttrChange int

const (
	AttrUnchanged AttrChange = iota
	AttrSet
	AttrClear
)

// Attributes is one Change Attributes request. Hidden/Archive/System are
// only meaningful on platforms with a real matching attribute bit — see
// setHidden/setArchive/setSystem (attrs_windows.go, attrs_darwin.go,
// attrs_unix.go), each a real implementation or a harmless no-op depending
// on the platform, so SetAttributes itself needs no runtime.GOOS branching;
// the UI layer decides which of these to even offer per platform.
//
// The 9 Owner/Group/Other × Read/Write/Execute fields are POSIX's own full
// permission model — the UI only offers these on macOS/Linux, in place of
// ReadOnly there (Windows keeps ReadOnly instead, its own closest concept).
// Unlike ReadOnly, these are NOT exempted from directories during
// recursion — see setPermissionBits's doc comment for why.
type Attributes struct {
	ReadOnly, Hidden, Archive, System   AttrChange
	OwnerRead, OwnerWrite, OwnerExecute AttrChange
	GroupRead, GroupWrite, GroupExecute AttrChange
	OtherRead, OtherWrite, OtherExecute AttrChange
	SetTime                             bool
	ModTime                             time.Time
}

// hasPermissionChange reports whether any of the 9 Owner/Group/Other
// permission fields request an actual change.
func (a Attributes) hasPermissionChange() bool {
	return a.OwnerRead != AttrUnchanged || a.OwnerWrite != AttrUnchanged || a.OwnerExecute != AttrUnchanged ||
		a.GroupRead != AttrUnchanged || a.GroupWrite != AttrUnchanged || a.GroupExecute != AttrUnchanged ||
		a.OtherRead != AttrUnchanged || a.OtherWrite != AttrUnchanged || a.OtherExecute != AttrUnchanged
}

// SetAttributes applies attrs to each of paths, recursing into
// subdirectories first if recurse is true — same one-flat-loop-over-sources
// shape Delete uses, with filepath.WalkDir doing the recursion when asked
// for, the same pattern totalSize/addDirToZip already use elsewhere in this
// file.
func SetAttributes(paths []string, recurse bool, attrs Attributes) error {
	// ReadOnly only ever applies to regular files, never directories: on
	// POSIX, clearing a directory's own write bit blocks adding, removing,
	// or renaming ANYTHING inside it — including undoing the change again
	// later — a far more disruptive lock than "read-only" means for a
	// folder on Windows, where the bit is largely cosmetic there. Hidden/
	// Archive/System/timestamp have no such asymmetry and apply to
	// directories too.
	apply := func(path string, isDir bool) error {
		if attrs.ReadOnly != AttrUnchanged && !isDir {
			if err := setReadOnly(path, attrs.ReadOnly == AttrSet); err != nil {
				return err
			}
		}
		if attrs.Hidden != AttrUnchanged {
			if err := setHidden(path, attrs.Hidden == AttrSet); err != nil {
				return err
			}
		}
		if attrs.Archive != AttrUnchanged {
			if err := setArchive(path, attrs.Archive == AttrSet); err != nil {
				return err
			}
		}
		if attrs.System != AttrUnchanged {
			if err := setSystem(path, attrs.System == AttrSet); err != nil {
				return err
			}
		}
		if attrs.hasPermissionChange() {
			if err := setPermissionBits(path, attrs); err != nil {
				return err
			}
		}
		if attrs.SetTime {
			if err := os.Chtimes(path, attrs.ModTime, attrs.ModTime); err != nil {
				return err
			}
		}
		return nil
	}

	for _, p := range paths {
		if !recurse {
			info, err := os.Lstat(p)
			if err != nil {
				return err
			}
			if err := apply(p, info.IsDir()); err != nil {
				return err
			}
			continue
		}
		err := filepath.WalkDir(p, func(walked string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return apply(walked, d.IsDir())
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// setReadOnly toggles the owner-write bit — the same bit localfs's
// entryFromInfo already reads back as vfs.Entry.ReadOnly, so this is
// symmetric with how the app already reports read-only elsewhere. On
// Windows, Go's os.Chmod maps the presence/absence of this one bit
// directly onto FILE_ATTRIBUTE_READONLY.
func setReadOnly(path string, on bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if on {
		mode &^= 0o200
	} else {
		mode |= 0o200
	}
	return os.Chmod(path, mode)
}

// applyBit returns mode with bit set, cleared, or left alone per change.
func applyBit(mode fs.FileMode, bit fs.FileMode, change AttrChange) fs.FileMode {
	switch change {
	case AttrSet:
		return mode | bit
	case AttrClear:
		return mode &^ bit
	default:
		return mode
	}
}

// setPermissionBits applies attrs' 9 Owner/Group/Other × Read/Write/Execute
// fields to path's real POSIX permission bits — the full chmod model,
// unlike ReadOnly's single simplified bit. Deliberately NOT exempted from
// directories the way ReadOnly is: someone reaching for the full
// permission grid has already opted into standard chmod-style control (the
// same UI a user would recognize from Finder's own Info panel or a
// terminal's chmod), and should get standard `chmod -R`-style behavior,
// directories included — unlike a simplified "read-only" toggle, this
// isn't a surprising side effect of something else.
func setPermissionBits(path string, attrs Attributes) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	mode = applyBit(mode, 0o400, attrs.OwnerRead)
	mode = applyBit(mode, 0o200, attrs.OwnerWrite)
	mode = applyBit(mode, 0o100, attrs.OwnerExecute)
	mode = applyBit(mode, 0o040, attrs.GroupRead)
	mode = applyBit(mode, 0o020, attrs.GroupWrite)
	mode = applyBit(mode, 0o010, attrs.GroupExecute)
	mode = applyBit(mode, 0o004, attrs.OtherRead)
	mode = applyBit(mode, 0o002, attrs.OtherWrite)
	mode = applyBit(mode, 0o001, attrs.OtherExecute)
	if mode == info.Mode().Perm() {
		return nil
	}
	return os.Chmod(path, mode)
}

// DirSize returns the total size of every file under path, walked
// recursively (du -s-style) — used by the "Calculate Folder Sizes" command.
func DirSize(path string) (int64, error) {
	return totalSize([]string{path})
}

func totalSize(paths []string) (int64, error) {
	var total int64
	for _, p := range paths {
		var pathTotal int64
		err := retryTransient(func() error {
			pathTotal = 0
			return filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					info, err := d.Info()
					if err != nil {
						return err
					}
					pathTotal += info.Size()
				}
				return nil
			})
		})
		if err != nil {
			return 0, err
		}
		total += pathTotal
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
	var entries []os.DirEntry
	err := retryTransient(func() error {
		e, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		entries = e
		return nil
	})
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
			if !progress(*done, total, src) {
				return ErrCancelled
			}
			return nil
		case ConflictCancel:
			return ErrCancelled
		case ConflictRename:
			dest = filepath.Join(filepath.Dir(dest), newName)
		case ConflictOverwrite:
			// proceed, OpenFile below truncates it.
		}
	}

	buf := make([]byte, 256*1024)
	var in, out *os.File
	var n int
	var rerr error

	// The open + very first read is exactly where a removable drive's
	// transient "wrong volume" condition (IsTransientRemovableMediaError)
	// surfaces, confirmed via a real Process Monitor capture — retry just
	// this step; the rest of the file streams through unprotected below,
	// since every observed failure happened on read #1, never mid-file.
	openErr := retryTransient(func() error {
		if in != nil {
			in.Close()
		}
		if out != nil {
			out.Close()
		}
		var err error
		in, err = os.Open(src)
		if err != nil {
			return err
		}
		out, err = os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		n, rerr = in.Read(buf)
		if rerr != nil && rerr != io.EOF {
			return rerr
		}
		return nil
	})
	if openErr != nil {
		return openErr
	}
	defer in.Close()
	defer out.Close()

	for {
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			*done += int64(n)
			if !progress(*done, total, src) {
				return ErrCancelled
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
		n, rerr = in.Read(buf)
	}
	return out.Close()
}
