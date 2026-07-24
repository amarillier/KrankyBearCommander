// Package zipfs is a read-only vfs.FileSystem backed by a single .zip
// archive, so a pane can browse into it exactly like a real directory
// (Reveal in Opposite Pane, F3 View, F5 Copy/Extract) without extracting
// the whole thing up front. It has no Fyne dependency and is unit-tested
// directly, matching internal/fsops's own convention.
//
// Paths this package hands out and accepts ("presented" paths) are the
// archive's own real host path, optionally followed by "/" and a path
// within the archive — e.g. "/Users/x/a.zip" (the archive's own root) or
// "/Users/x/a.zip/docs/readme.txt". This lets every existing caller
// (tabLabel, the status line, Copy Path, ...) keep treating fileListView's
// state.Path as an ordinary string, no special-casing required — the only
// place that needs to know about the "real path vs. archive-internal path"
// split is this package itself (see internalPath) and fileListView's
// enter/exit-archive navigation (see adjustFSForTarget in filelist.go).
package zipfs

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"commander/internal/fsops"
	"commander/internal/vfs"
)

// ErrReadOnly is returned by every mutating operation — an archive can be
// browsed and extracted from, never written to in place.
var ErrReadOnly = errors.New("archive is read-only")

// HasZipExt reports whether name looks like a .zip archive by extension —
// the only heuristic used to decide whether double-clicking a file should
// browse into it rather than open it with the OS's default application.
func HasZipExt(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".zip")
}

// node is one entry in the archive's synthesized directory tree — zip
// files store a flat list of slash-separated paths, not a real tree, and
// don't always have explicit entries for intermediate directories.
type node struct {
	name     string
	isDir    bool
	size     int64
	modTime  int64 // Unix seconds; avoids importing time just for zero-value comparisons
	zipFile  *zip.File
	children map[string]*node
}

// FS is a single open archive, rooted at zipPath on the host filesystem.
type FS struct {
	zipPath string
	rc      *zip.ReadCloser
	root    *node
}

// Open opens the archive at zipPath and builds its directory tree. Callers
// must Close it once done (when navigating back out — see filelist.go's
// adjustFSForTarget).
func Open(zipPath string) (*FS, error) {
	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	root := &node{isDir: true, children: map[string]*node{}}
	for _, f := range rc.File {
		insert(root, f)
	}
	return &FS{zipPath: zipPath, rc: rc, root: root}, nil
}

func insert(root *node, f *zip.File) {
	parts := strings.Split(strings.Trim(f.Name, "/"), "/")
	cur := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		child, ok := cur.children[part]
		if !ok {
			child = &node{name: part, children: map[string]*node{}}
			cur.children[part] = child
		}
		if i == len(parts)-1 && !f.FileInfo().IsDir() {
			child.isDir = false
			child.size = int64(f.UncompressedSize64)
			child.modTime = f.Modified.Unix()
			child.zipFile = f
		} else {
			child.isDir = true
		}
		cur = child
	}
}

// Close releases the underlying archive file handle.
func (fs *FS) Close() error {
	return fs.rc.Close()
}

// ZipPath is the archive's own real, on-disk path.
func (fs *FS) ZipPath() string {
	return fs.zipPath
}

// IsInside reports whether presented (the archive's own root, or a path
// within it) still falls under this archive — used by fileListView to
// detect when a navigation target (typically ".." from the archive's own
// root) has escaped back out to the real filesystem.
func (fs *FS) IsInside(presented string) bool {
	return presented == fs.zipPath || strings.HasPrefix(presented, fs.zipPath+"/")
}

// AllFiles returns the presented path of every file (non-directory) entry
// in the archive, sorted — used by the F3 "preview one member" picker,
// which just needs a flat list rather than hierarchical browsing.
func (fs *FS) AllFiles() []string {
	var paths []string
	var walk func(prefix string, n *node)
	walk = func(prefix string, n *node) {
		for name, c := range n.children {
			p := prefix + "/" + name
			if c.isDir {
				walk(p, c)
			} else {
				paths = append(paths, fs.zipPath+p)
			}
		}
	}
	walk("", fs.root)
	sort.Strings(paths)
	return paths
}

// internalPath converts a presented path to one relative to the archive's
// own root (empty string for the root itself).
func (fs *FS) internalPath(presented string) string {
	return strings.TrimPrefix(strings.TrimPrefix(presented, fs.zipPath), "/")
}

func (fs *FS) lookup(internal string) (*node, error) {
	cur := fs.root
	internal = strings.Trim(internal, "/")
	if internal == "" || internal == "." {
		return cur, nil
	}
	for _, part := range strings.Split(internal, "/") {
		if part == "" {
			continue
		}
		next, ok := cur.children[part]
		if !ok {
			return nil, os.ErrNotExist
		}
		cur = next
	}
	return cur, nil
}

func entryFromNode(n *node) vfs.Entry {
	mode := os.FileMode(0o444)
	if n.isDir {
		mode |= os.ModeDir | 0o111 // +x so directories look traversable, matching os.Stat's usual dir mode
	}
	return vfs.Entry{
		Name:     n.name,
		Size:     n.size,
		ModTime:  time.Unix(n.modTime, 0),
		IsDir:    n.isDir,
		ReadOnly: true,
		Mode:     mode,
	}
}

func (fs *FS) ReadDir(presented string) ([]vfs.Entry, error) {
	n, err := fs.lookup(fs.internalPath(presented))
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(n.children))
	for _, c := range n.children {
		entries = append(entries, entryFromNode(c))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func (fs *FS) Stat(presented string) (vfs.Entry, error) {
	n, err := fs.lookup(fs.internalPath(presented))
	if err != nil {
		return vfs.Entry{}, err
	}
	return entryFromNode(n), nil
}

func (fs *FS) Open(presented string) (io.ReadCloser, error) {
	n, err := fs.lookup(fs.internalPath(presented))
	if err != nil {
		return nil, err
	}
	if n.zipFile == nil {
		return nil, errors.New(presented + ": is a directory")
	}
	return n.zipFile.Open()
}

func (fs *FS) Create(string) (io.WriteCloser, error) { return nil, ErrReadOnly }
func (fs *FS) Mkdir(string) error                    { return ErrReadOnly }
func (fs *FS) Remove(string) error                   { return ErrReadOnly }
func (fs *FS) Rename(string, string) error           { return ErrReadOnly }

// Join concatenates presented paths — the archive-internal split only
// matters to lookups (ReadDir/Stat/Open/Dir), not to plain concatenation.
func (fs *FS) Join(elem ...string) string { return path.Join(elem...) }

// Dir returns presented's parent — within the archive, ordinary path.Dir
// semantics; at the archive's own root, the REAL directory containing the
// .zip file, so ".." naturally walks back out of it. fileListView notices
// this via IsInside and swaps back to a real vfs.FileSystem accordingly.
func (fs *FS) Dir(presented string) string {
	internal := fs.internalPath(presented)
	if internal == "" || internal == "." {
		return filepath.Dir(fs.zipPath)
	}
	parent := path.Dir(internal)
	if parent == "." {
		return fs.zipPath
	}
	return fs.zipPath + "/" + parent
}

func (fs *FS) HomeDir() (string, error) { return "", errors.New("no home directory inside an archive") }
func (fs *FS) Roots() ([]string, error) { return []string{fs.zipPath}, nil }

// Extract copies each of sources (presented paths, files and/or
// directories, recursively) into destDir on the real filesystem, preserving
// each source's base name — the archive-browsing equivalent of F5 Copy.
// Its signature deliberately matches fsops's Copy/Move so the same
// progress-dialog/conflict-resolution UI (fileops_ui.go's runFileOp) drives
// it unmodified.
func (fs *FS) Extract(sources []string, destDir string, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	if progress == nil {
		progress = func(int64, int64, string) {}
	}
	if resolve == nil {
		resolve = func(string) (fsops.ConflictAction, string) { return fsops.ConflictOverwrite, "" }
	}

	nodes := make([]*node, 0, len(sources))
	var total int64
	for _, src := range sources {
		n, err := fs.lookup(fs.internalPath(src))
		if err != nil {
			return err
		}
		nodes = append(nodes, n)
		total += n.totalSize()
	}

	var done int64
	for _, n := range nodes {
		if err := fs.extractNode(n, filepath.Join(destDir, n.name), &done, total, progress, resolve); err != nil {
			return err
		}
	}
	return nil
}

func (n *node) totalSize() int64 {
	if !n.isDir {
		return n.size
	}
	var total int64
	for _, c := range n.children {
		total += c.totalSize()
	}
	return total
}

func (fs *FS) extractNode(n *node, dest string, done *int64, total int64, progress fsops.ProgressFunc, resolve fsops.ConflictFunc) error {
	if n.isDir {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		for _, c := range n.children {
			if err := fs.extractNode(c, filepath.Join(dest, c.name), done, total, progress, resolve); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := os.Lstat(dest); err == nil {
		action, newName := resolve(dest)
		switch action {
		case fsops.ConflictSkip:
			*done += n.size
			progress(*done, total, dest)
			return nil
		case fsops.ConflictCancel:
			return fsops.ErrCancelled
		case fsops.ConflictRename:
			dest = filepath.Join(filepath.Dir(dest), newName)
		case fsops.ConflictOverwrite:
			// proceed, os.Create below truncates it.
		}
	}

	in, err := n.zipFile.Open()
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 256*1024)
	for {
		nr, rerr := in.Read(buf)
		if nr > 0 {
			if _, werr := out.Write(buf[:nr]); werr != nil {
				return werr
			}
			*done += int64(nr)
			progress(*done, total, dest)
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
