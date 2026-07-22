package panelstate

import (
	"testing"
	"time"

	"commander/internal/vfs"
)

func TestUnlockedNavigateFreely(t *testing.T) {
	s := New("/home/user")
	if !s.Navigate("/home/user/docs") {
		t.Fatal("unlocked tab should navigate freely")
	}
	if s.Path != "/home/user/docs" {
		t.Fatalf("Path = %q, want /home/user/docs", s.Path)
	}
}

func TestUnlockedHomeTargetIsDefault(t *testing.T) {
	s := New("/home/user/docs")
	if got := s.HomeTarget("/home/user"); got != "/home/user" {
		t.Fatalf("HomeTarget = %q, want /home/user", got)
	}
}

func TestLockedAllowNavigation_CanDescendButHomeSnapsToLockedRoot(t *testing.T) {
	s := New("/srv/data")
	s.Lock(true)

	if !s.Navigate("/srv/data/reports") {
		t.Fatal("locked+allowNavigation should permit descending into subdirectories")
	}
	if got := s.HomeTarget("/home/user"); got != "/srv/data" {
		t.Fatalf("HomeTarget = %q, want locked root /srv/data", got)
	}
}

func TestLockedDenyNavigation_RefusesDirectoryChange(t *testing.T) {
	s := New("/srv/data")
	s.Lock(false)

	if s.Navigate("/srv/data/reports") {
		t.Fatal("locked+denyNavigation should refuse the directory change")
	}
	if s.Path != "/srv/data" {
		t.Fatalf("Path changed to %q despite navigation being denied", s.Path)
	}
	if s.CanNavigate() {
		t.Fatal("CanNavigate should be false when locked without navigation")
	}
}

func TestJumpBypassesLockWithoutChangingIt(t *testing.T) {
	s := New("/srv/data")
	s.Lock(false) // fully pinned: Navigate would refuse any directory change

	s.Jump("/home/user/Desktop")

	if s.Path != "/home/user/Desktop" {
		t.Fatalf("Jump should change Path even when locked, got %q", s.Path)
	}
	if !s.Locked || s.LockedRoot != "/srv/data" || s.AllowNavigation {
		t.Fatalf("Jump must not alter the lock itself, got Locked=%v LockedRoot=%q AllowNavigation=%v",
			s.Locked, s.LockedRoot, s.AllowNavigation)
	}
}

func TestJumpClearsSelectionAndCursor(t *testing.T) {
	s := New("/home/user")
	s.ToggleSelect("a.txt")
	s.Cursor = "a.txt"

	s.Jump("/home/user/docs")

	if len(s.Selected) != 0 || s.Cursor != "" {
		t.Fatalf("Jump should clear selection/cursor like Navigate, got Selected=%v Cursor=%q", s.Selected, s.Cursor)
	}
}

func TestHomeTargetReturnsLockedRootEvenWhenNavigationDenied(t *testing.T) {
	s := New("/srv/data")
	s.Lock(false) // fully pinned

	// Home is implemented as a Jump (see fileListView.Home), so it must
	// still resolve to the locked root even though Navigate itself would
	// refuse to go anywhere — that's the whole point of a locked tab's Home:
	// getting back after an explicit detour (e.g. a Favorites jump).
	if got := s.HomeTarget("/home/user"); got != "/srv/data" {
		t.Fatalf("HomeTarget = %q, want locked root /srv/data even when navigation is denied", got)
	}
}

func TestUnlockRestoresFreeNavigation(t *testing.T) {
	s := New("/srv/data")
	s.Lock(false)
	s.Unlock()

	if !s.Navigate("/anywhere") {
		t.Fatal("unlocking should restore free navigation")
	}
}

func TestNavigateClearsSelectionAndCursor(t *testing.T) {
	s := New("/home/user")
	s.ToggleSelect("a.txt")
	s.Cursor = "a.txt"

	s.Navigate("/home/user/docs")

	if len(s.Selected) != 0 {
		t.Fatalf("selection not cleared after Navigate: %v", s.Selected)
	}
	if s.Cursor != "" {
		t.Fatalf("cursor not cleared after Navigate: %q", s.Cursor)
	}
}

func TestToggleSelect(t *testing.T) {
	s := New("/home/user")
	s.ToggleSelect("a.txt")
	if !s.Selected["a.txt"] {
		t.Fatal("expected a.txt selected")
	}
	s.ToggleSelect("a.txt")
	if s.Selected["a.txt"] {
		t.Fatal("expected a.txt deselected after second toggle")
	}
}

func TestToggleSortSameFieldFlipsDirection(t *testing.T) {
	s := New("/home/user")
	if s.SortField != SortName || !s.SortAscending {
		t.Fatalf("default sort should be Name ascending, got field=%v asc=%v", s.SortField, s.SortAscending)
	}
	s.ToggleSort(SortName)
	if s.SortAscending {
		t.Fatal("clicking the active sort field again should flip to descending")
	}
	s.ToggleSort(SortName)
	if !s.SortAscending {
		t.Fatal("clicking the active sort field a third time should flip back to ascending")
	}
}

func TestToggleSortDifferentFieldDefaultsAscending(t *testing.T) {
	s := New("/home/user")
	s.ToggleSort(SortName) // now descending
	s.ToggleSort(SortSize) // switching field should reset to ascending
	if s.SortField != SortSize || !s.SortAscending {
		t.Fatalf("expected SortSize ascending, got field=%v asc=%v", s.SortField, s.SortAscending)
	}
}

func mkEntry(name string, isDir bool, size int64, mod time.Time) vfs.Entry {
	return vfs.Entry{Name: name, IsDir: isDir, Size: size, ModTime: mod}
}

func TestSortEntriesDirectoriesFirst(t *testing.T) {
	now := time.Now()
	entries := []vfs.Entry{
		mkEntry("zeta.txt", false, 1, now),
		mkEntry("alpha", true, 0, now),
		mkEntry("beta.txt", false, 1, now),
		mkEntry("omega", true, 0, now),
	}
	sorted := SortEntries(entries, SortName, true)
	want := []string{"alpha", "omega", "beta.txt", "zeta.txt"}
	for i, name := range want {
		if sorted[i].Name != name {
			t.Fatalf("sorted[%d] = %q, want %q (full: %v)", i, sorted[i].Name, name, namesOf(sorted))
		}
	}
}

func TestSortEntriesBySizeDescending(t *testing.T) {
	now := time.Now()
	entries := []vfs.Entry{
		mkEntry("small.txt", false, 10, now),
		mkEntry("large.txt", false, 1000, now),
		mkEntry("medium.txt", false, 100, now),
	}
	sorted := SortEntries(entries, SortSize, false)
	want := []string{"large.txt", "medium.txt", "small.txt"}
	for i, name := range want {
		if sorted[i].Name != name {
			t.Fatalf("sorted[%d] = %q, want %q (full: %v)", i, sorted[i].Name, name, namesOf(sorted))
		}
	}
}

func TestSortEntriesByExtension(t *testing.T) {
	now := time.Now()
	entries := []vfs.Entry{
		mkEntry("report.zip", false, 1, now),
		mkEntry("notes.txt", false, 1, now),
		mkEntry("readme", false, 1, now),
		mkEntry(".bashrc", false, 1, now),
	}
	sorted := SortEntries(entries, SortExt, true)
	want := []string{".bashrc", "readme", "notes.txt", "report.zip"}
	for i, name := range want {
		if sorted[i].Name != name {
			t.Fatalf("sorted[%d] = %q, want %q (full: %v)", i, sorted[i].Name, name, namesOf(sorted))
		}
	}
}

func namesOf(entries []vfs.Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}
