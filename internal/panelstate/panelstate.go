// Package panelstate holds one pane-tab's navigation/view/selection state as
// plain data + logic, with no Fyne dependency, so the locked-navigation rules
// (the trickiest behavior in the file manager) can be unit-tested directly.
package panelstate

import (
	"sort"
	"strings"

	"commander/internal/vfs"
)

// ViewMode selects how a tab's listing is rendered.
type ViewMode int

const (
	ViewBrief ViewMode = iota
	ViewExpanded
)

// SortField selects which column/attribute entries are ordered by. Within
// any field, directories are always grouped before files (standard commander
// convention), and ".." (parent) always sorts first of all.
type SortField int

const (
	SortName SortField = iota
	SortExt
	SortSize
	SortModified
)

// State is one tab's navigation/view/selection state.
type State struct {
	Path string // current directory, backend-native path

	Locked          bool
	LockedRoot      string
	AllowNavigation bool // meaningful only when Locked

	ViewMode      ViewMode
	SortField     SortField
	SortAscending bool

	Selected map[string]bool // selected entry names within Path
	Cursor   string          // name of the cursor row within Path
}

// New returns a fresh, unlocked tab state rooted at path.
func New(path string) *State {
	return &State{
		Path:          path,
		SortField:     SortName,
		SortAscending: true,
		Selected:      map[string]bool{},
	}
}

// Lock pins the tab to its current directory. allowNavigation controls
// whether the user may still cd into subdirectories (see package doc /
// CLAUDE-adjacent plan notes): true permits free navigation below the lock
// (with Home/"\"/"/" snapping back to it), false refuses all navigation.
func (s *State) Lock(allowNavigation bool) {
	s.Locked = true
	s.LockedRoot = s.Path
	s.AllowNavigation = allowNavigation
}

// Unlock releases the tab to navigate anywhere.
func (s *State) Unlock() {
	s.Locked = false
	s.LockedRoot = ""
}

// CanNavigate reports whether the user is currently allowed to change
// directory in this tab at all.
func (s *State) CanNavigate() bool {
	return !s.Locked || s.AllowNavigation
}

// Navigate changes the tab's current directory to target, clearing selection
// and cursor. It refuses (returning false, leaving Path unchanged) when the
// tab is locked with navigation denied. This is "casual" in-pane browsing
// (double-click/Enter into a subdirectory or ".."), which a full lock is
// meant to prevent — see Jump for the explicit-destination exception.
func (s *State) Navigate(target string) bool {
	if !s.CanNavigate() {
		return false
	}
	s.Path = target
	s.Selected = map[string]bool{}
	s.Cursor = ""
	return true
}

// Jump changes the tab's current directory to target unconditionally,
// ignoring any lock — for explicit "take me here" actions (Favorites,
// Volumes, Home) rather than casual in-pane browsing. It never touches
// Locked/LockedRoot/AllowNavigation, so a locked tab's Home target (and any
// re-lock) is unaffected by wherever a jump lands; the lock only ever
// changes via Lock/Unlock.
func (s *State) Jump(target string) {
	s.Path = target
	s.Selected = map[string]bool{}
	s.Cursor = ""
}

// HomeTarget returns where Home / "\" / "/" should navigate to: the locked
// root when locked, otherwise defaultHome (the caller's resolved filesystem
// root or user home directory preference). Home is a Jump (see above), so it
// works even when the tab denies casual navigation — that's the whole point
// of a locked tab's Home button: getting back after an explicit detour
// (e.g. a Favorites jump) that Navigate itself would have refused.
func (s *State) HomeTarget(defaultHome string) string {
	if s.Locked {
		return s.LockedRoot
	}
	return defaultHome
}

// ToggleSelect flips whether name is selected.
func (s *State) ToggleSelect(name string) {
	if s.Selected[name] {
		delete(s.Selected, name)
	} else {
		s.Selected[name] = true
	}
}

// ClearSelection empties the selection set.
func (s *State) ClearSelection() {
	s.Selected = map[string]bool{}
}

// ToggleSort sets the sort field, flipping direction if it's already the
// active field (click-same-header-again behavior), otherwise switching field
// and defaulting to ascending.
func (s *State) ToggleSort(field SortField) {
	if s.SortField == field {
		s.SortAscending = !s.SortAscending
		return
	}
	s.SortField = field
	s.SortAscending = true
}

// SortEntries orders entries per field/ascending, always grouping
// directories before files, both alphabetically-tiebroken.
func SortEntries(entries []vfs.Entry, field SortField, ascending bool) []vfs.Entry {
	out := make([]vfs.Entry, len(entries))
	copy(out, entries)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IsDir != b.IsDir {
			return a.IsDir // directories first regardless of sort direction
		}
		less := lessBy(a, b, field)
		if !ascending {
			return !less
		}
		return less
	})
	return out
}

func lessBy(a, b vfs.Entry, field SortField) bool {
	switch field {
	case SortSize:
		if a.Size != b.Size {
			return a.Size < b.Size
		}
	case SortModified:
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.Before(b.ModTime)
		}
	case SortExt:
		ae, be := extOf(a.Name), extOf(b.Name)
		if ae != be {
			return ae < be
		}
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}

func extOf(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 { // no dot, or dotfile with no extension (".bashrc")
		return ""
	}
	return strings.ToLower(name[i+1:])
}
