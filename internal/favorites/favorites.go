// Package favorites persists a user-managed list of bookmarked directories
// (the Favorites button's popup, alongside the filesystem's Volumes/roots —
// see paneview.go). It has no Fyne dependency, matching this repo's other
// internal packages.
package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// Entry is one bookmarked directory.
type Entry struct {
	Label string `json:"label"` // shown in the Favorites menu; defaults to the path's last component
	Path  string `json:"path"`
}

// List is the persisted set of favorites.
type List struct {
	Entries []Entry `json:"entries"`
}

// DefaultPath returns the per-user path favorites.json lives at, namespaced
// by appName the same way internal/layout namespaces the window layout.
func DefaultPath(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "favorites.json"), nil
}

// Load reads and parses path. A missing file is not an error: it returns an
// empty List so first-run callers can seed it.
func Load(path string) (List, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return List{}, nil
		}
		return List{}, err
	}
	var l List
	if err := json.Unmarshal(b, &l); err != nil {
		return List{}, err
	}
	return l, nil
}

// Save writes l to path as JSON, creating parent directories as needed.
func Save(path string, l List) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Has reports whether path is already favorited.
func (l List) Has(path string) bool {
	for _, e := range l.Entries {
		if e.Path == path {
			return true
		}
	}
	return false
}

// Add appends a favorite, doing nothing if path is already present.
func (l *List) Add(label, path string) {
	if l.Has(path) {
		return
	}
	l.Entries = append(l.Entries, Entry{Label: label, Path: path})
}

// Remove drops path from the list, if present.
func (l *List) Remove(path string) {
	out := l.Entries[:0]
	for _, e := range l.Entries {
		if e.Path != path {
			out = append(out, e)
		}
	}
	l.Entries = out
}

// DefaultSeedCandidates returns a first-run starting point for home's
// platform: the home directory itself, common user directories (Desktop,
// Downloads, ...), plus, on macOS, /Applications. Callers should filter to
// paths that actually exist
// before saving them as real favorites — a fresh/minimal system may be
// missing some of these (e.g. no Desktop folder on a headless Linux
// install), and this package has no filesystem-existence dependency of its
// own to keep it consistent with this repo's other pure internal packages.
func DefaultSeedCandidates(home string) []Entry {
	switch runtime.GOOS {
	case "darwin":
		return []Entry{
			{Label: "Home", Path: home},
			{Label: "Applications", Path: "/Applications"},
			{Label: "Desktop", Path: filepath.Join(home, "Desktop")},
			{Label: "Downloads", Path: filepath.Join(home, "Downloads")},
		}
	case "windows":
		return []Entry{
			{Label: "Home", Path: home},
			{Label: "Desktop", Path: filepath.Join(home, "Desktop")},
			{Label: "Downloads", Path: filepath.Join(home, "Downloads")},
			{Label: "Documents", Path: filepath.Join(home, "Documents")},
		}
	default: // Linux and other Unix desktops
		return []Entry{
			{Label: "Home", Path: home},
			{Label: "Desktop", Path: filepath.Join(home, "Desktop")},
			{Label: "Downloads", Path: filepath.Join(home, "Downloads")},
			{Label: "Documents", Path: filepath.Join(home, "Documents")},
		}
	}
}
