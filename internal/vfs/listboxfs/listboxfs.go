// Package listboxfs is a flat, non-hierarchical vfs.FileSystem presenting an
// arbitrary set of real files that can each live in a different real
// directory — e.g. Search's "Feed to Listbox" (search_ui.go), TotalCmd's
// eponymous feature. Unlike zipfs, it's not read-only: every mutating
// operation (and Open/Stat) delegates straight to the real file underneath
// via localfs, so Copy/Move/Rename/Delete behave exactly as if you'd
// navigated to each file's own real directory — this only changes what
// ReadDir *shows*, never where anything actually lives.
//
// Paths this package hands out and accepts are its own synthetic root
// string (see New) for the listing itself, or — via Join — the entry's real
// absolute host path directly. That second part matters: most of this app's
// file operations (fsops, the viewer, drag-out, ...) build a path via
// view.fs.Join(view.state.Path, entry.Name) and then use it directly against
// the real OS (os.Open, os.Rename, ...) without ever asking the
// vfs.FileSystem to do the work — so unlike zipfs (whose Join produces a
// path only zipfs itself can resolve, requiring type-switches throughout the
// app), this package's Join must yield an already-real, already-openable
// path, or all of those callers would silently break.
package listboxfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"commander/internal/vfs"
	"commander/internal/vfs/localfs"
)

// FS is one search-results-style flat listing.
type FS struct {
	real   vfs.FileSystem // localfs.New() — every real op delegates here
	root   string
	byName map[string]string // display name -> real absolute path
}

// New returns a listing rooted at root (a synthetic label, not a real path —
// see the package doc) presenting exactly the given entries: byName maps
// each entry's display name (see search_ui.go's listboxNames, which
// disambiguates any name collision — every name here must be unique) to its
// real absolute host path.
func New(root string, byName map[string]string) *FS {
	return &FS{real: localfs.New(), root: root, byName: byName}
}

// IsInside reports whether target is still this listing's own root — there's
// no real hierarchy to stay "inside" of the way an archive has, so any OTHER
// target means navigation has moved on from the listbox view entirely.
// Satisfies the swappableFS interface (filelist.go), which
// adjustFSForTarget uses to swap back to a real vfs.FileSystem.
func (fs *FS) IsInside(target string) bool {
	return target == fs.root
}

// Close is a no-op — a listbox holds no real handle to release, unlike an
// open archive or a live remote connection. Satisfies swappableFS.
func (fs *FS) Close() error {
	return nil
}

func (fs *FS) ReadDir(path string) ([]vfs.Entry, error) {
	if path != fs.root {
		return nil, fmt.Errorf("listboxfs: not a directory: %s", path)
	}
	entries := make([]vfs.Entry, 0, len(fs.byName))
	for name, real := range fs.byName {
		info, err := os.Lstat(real)
		if err != nil {
			continue // vanished since the search ran — drop it silently rather than fail the whole listing
		}
		entries = append(entries, vfs.Entry{
			Name:     name,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			IsDir:    info.IsDir(),
			Mode:     info.Mode(),
			ReadOnly: info.Mode().Perm()&0o200 == 0,
		})
	}
	return entries, nil
}

func (fs *FS) Stat(path string) (vfs.Entry, error) {
	if path == fs.root {
		return vfs.Entry{Name: fs.root, IsDir: true}, nil
	}
	return fs.real.Stat(path) // path is already a real host path — see Join
}

func (fs *FS) Open(path string) (io.ReadCloser, error)    { return fs.real.Open(path) }
func (fs *FS) Create(path string) (io.WriteCloser, error) { return fs.real.Create(path) }
func (fs *FS) Mkdir(path string) error                    { return fs.real.Mkdir(path) }
func (fs *FS) Remove(path string) error                   { return fs.real.Remove(path) }
func (fs *FS) Rename(oldPath, newPath string) error       { return fs.real.Rename(oldPath, newPath) }

// Join resolves root+name to the entry's real absolute path (see the
// package doc for why this must be real, not synthetic) — everything else
// falls back to a plain filepath.Join, which covers both an already-real
// path (e.g. re-joining a path this same Join already resolved) and an
// unrecognized name degrading gracefully rather than producing an unusable
// path.
func (fs *FS) Join(elem ...string) string {
	if len(elem) < 2 || elem[0] != fs.root {
		return filepath.Join(elem...)
	}
	if real, ok := fs.byName[elem[1]]; ok {
		return filepath.Join(append([]string{real}, elem[2:]...)...)
	}
	return filepath.Join(elem[1:]...)
}

// Dir returns root's own parent as root itself (there's no real parent to
// walk "up" into — see Root's doc comment) for the root; any other path is
// already real (see Join), so it gets ordinary filepath.Dir semantics.
func (fs *FS) Dir(path string) string {
	if path == fs.root {
		return fs.root
	}
	return filepath.Dir(path)
}

func (fs *FS) HomeDir() (string, error) { return fs.real.HomeDir() }
func (fs *FS) Roots() ([]string, error) { return fs.real.Roots() }
