// Package vfs abstracts filesystem access behind an interface so the pane/UI
// code (package main) never talks to os.* directly. Phase 1 ships only the
// local backend (internal/vfs/localfs); Phase 2 adds SFTP/FTP/etc backends
// behind this same interface without touching pane or UI code.
package vfs

import (
	"io"
	"time"
)

// Entry describes one directory entry as shown in a pane listing.
type Entry struct {
	Name     string
	Size     int64
	ModTime  time.Time
	IsDir    bool
	ReadOnly bool
}

// FileSystem is the minimal set of operations a pane needs to browse and
// manipulate a tree of files. Paths are backend-native (e.g. "/" separators
// for local Unix, remote backends define their own convention).
type FileSystem interface {
	ReadDir(path string) ([]Entry, error)
	Stat(path string) (Entry, error)
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Mkdir(path string) error
	Remove(path string) error
	Rename(oldPath, newPath string) error

	// Join joins path elements using this backend's separator.
	Join(elem ...string) string
	// Dir returns the parent directory of path.
	Dir(path string) string

	// HomeDir returns the user's home directory, if this backend has one.
	HomeDir() (string, error)
	// Roots returns the top-level entry points for navigation: "/" on Unix,
	// or one entry per drive letter on Windows.
	Roots() ([]string, error)
}
