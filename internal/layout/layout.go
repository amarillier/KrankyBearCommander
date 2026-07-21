// Package layout persists the dual-pane window's tab/pane arrangement (which
// directories are open, lock state, view mode, sort) across launches, as a
// small JSON file. Fyne's Preferences API (used elsewhere in this app for
// flat scalars like the light/dark theme choice) isn't a good fit for this
// nested, per-tab data, so layout uses its own file instead — same idea as
// util.go's updateCheckStatePath, just for a different piece of state.
package layout

import (
	"encoding/json"
	"os"
	"path/filepath"

	"commander/internal/panelstate"
)

// TabLayout is one tab's persisted state.
type TabLayout struct {
	Path            string               `json:"path"`
	Locked          bool                 `json:"locked"`
	LockedRoot      string               `json:"lockedRoot"`
	AllowNavigation bool                 `json:"allowNavigation"`
	ViewMode        panelstate.ViewMode  `json:"viewMode"`
	SortField       panelstate.SortField `json:"sortField"`
	SortAscending   bool                 `json:"sortAscending"`
}

// PaneLayout is one pane's (left or right) full tab strip.
type PaneLayout struct {
	Tabs      []TabLayout `json:"tabs"`
	ActiveTab int         `json:"activeTab"`
}

// Layout is the whole persisted window arrangement.
type Layout struct {
	Left        PaneLayout `json:"left"`
	Right       PaneLayout `json:"right"`
	SplitOffset float64    `json:"splitOffset"` // matches container.Split.Offset, 0..1
}

// FromState captures a panelstate.State as a TabLayout.
func FromState(s *panelstate.State) TabLayout {
	return TabLayout{
		Path:            s.Path,
		Locked:          s.Locked,
		LockedRoot:      s.LockedRoot,
		AllowNavigation: s.AllowNavigation,
		ViewMode:        s.ViewMode,
		SortField:       s.SortField,
		SortAscending:   s.SortAscending,
	}
}

// ToState reconstructs a panelstate.State from a persisted TabLayout.
func (t TabLayout) ToState() *panelstate.State {
	s := panelstate.New(t.Path)
	s.Locked = t.Locked
	s.LockedRoot = t.LockedRoot
	s.AllowNavigation = t.AllowNavigation
	s.ViewMode = t.ViewMode
	s.SortField = t.SortField
	s.SortAscending = t.SortAscending
	return s
}

// DefaultPath returns the per-user path layout.json lives at, namespaced by
// appName the same way util.go's updateCheckStatePath namespaces its cache.
func DefaultPath(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "layout.json"), nil
}

// Load reads and parses path. A missing file is not an error: it returns the
// zero Layout so callers can fall back to a sensible first-launch default.
func Load(path string) (Layout, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Layout{}, nil
		}
		return Layout{}, err
	}
	var l Layout
	if err := json.Unmarshal(b, &l); err != nil {
		return Layout{}, err
	}
	return l, nil
}

// Save writes l to path as JSON, creating parent directories as needed.
func Save(path string, l Layout) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
