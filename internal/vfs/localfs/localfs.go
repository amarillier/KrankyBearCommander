// Package localfs is the os-backed vfs.FileSystem implementation used for all
// local browsing in Phase 1 (and for the "local side" of any Phase 2 remote
// backend).
package localfs

import (
	"io"
	"os"
	"path/filepath"
	"runtime"

	"commander/internal/vfs"
)

// FS implements vfs.FileSystem over the local operating system filesystem.
type FS struct{}

// New returns a local filesystem backend.
func New() *FS { return &FS{} }

func (FS) ReadDir(path string) ([]vfs.Entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			// Entry vanished or is unreadable (e.g. broken symlink) between
			// ReadDir and Info; skip it rather than failing the whole listing.
			continue
		}
		entries = append(entries, entryFromInfo(de.Name(), info))
	}
	return entries, nil
}

func (FS) Stat(path string) (vfs.Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return vfs.Entry{}, err
	}
	return entryFromInfo(info.Name(), info), nil
}

func entryFromInfo(name string, info os.FileInfo) vfs.Entry {
	return vfs.Entry{
		Name:     name,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		IsDir:    info.IsDir(),
		Mode:     info.Mode(),
		ReadOnly: info.Mode().Perm()&0o200 == 0,
	}
}

func (FS) Open(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func (FS) Create(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

func (FS) Mkdir(path string) error {
	return os.Mkdir(path, 0o755)
}

func (FS) Remove(path string) error {
	return os.Remove(path)
}

func (FS) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (FS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (FS) Dir(path string) string {
	return filepath.Dir(path)
}

func (FS) HomeDir() (string, error) {
	return os.UserHomeDir()
}

// Roots returns "/" on Unix-like systems, or one entry per currently
// accessible drive letter (e.g. "C:\\") on Windows.
func (FS) Roots() ([]string, error) {
	if runtime.GOOS != "windows" {
		return []string{"/"}, nil
	}
	var roots []string
	for c := byte('A'); c <= 'Z'; c++ {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err == nil {
			roots = append(roots, root)
		}
	}
	return roots, nil
}
