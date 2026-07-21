package layout

import (
	"path/filepath"
	"testing"

	"commander/internal/panelstate"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "layout.json")

	want := Layout{
		Left: PaneLayout{
			Tabs: []TabLayout{
				{Path: "/home/user", ViewMode: panelstate.ViewExpanded, SortField: panelstate.SortSize, SortAscending: false},
				{Path: "/srv/data", Locked: true, LockedRoot: "/srv/data", AllowNavigation: true},
			},
			ActiveTab: 1,
		},
		Right: PaneLayout{
			Tabs:      []TabLayout{{Path: "/tmp"}},
			ActiveTab: 0,
		},
		SplitOffset: 0.5,
	}

	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SplitOffset != want.SplitOffset {
		t.Fatalf("SplitOffset = %v, want %v", got.SplitOffset, want.SplitOffset)
	}
	if len(got.Left.Tabs) != 2 || got.Left.ActiveTab != 1 {
		t.Fatalf("Left pane not round-tripped: %+v", got.Left)
	}
	if got.Left.Tabs[1].Locked != true || got.Left.Tabs[1].LockedRoot != "/srv/data" {
		t.Fatalf("locked tab not round-tripped: %+v", got.Left.Tabs[1])
	}
	if len(got.Right.Tabs) != 1 || got.Right.Tabs[0].Path != "/tmp" {
		t.Fatalf("Right pane not round-tripped: %+v", got.Right)
	}
}

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if len(got.Left.Tabs) != 0 || len(got.Right.Tabs) != 0 {
		t.Fatalf("expected zero-value Layout, got %+v", got)
	}
}

func TestStateConversionRoundTrip(t *testing.T) {
	s := panelstate.New("/home/user")
	s.Lock(true)
	s.ToggleSort(panelstate.SortModified)

	tl := FromState(s)
	back := tl.ToState()

	if back.Path != s.Path || back.Locked != s.Locked || back.LockedRoot != s.LockedRoot {
		t.Fatalf("round-tripped state mismatch: got %+v, want %+v", back, s)
	}
	if back.SortField != s.SortField || back.SortAscending != s.SortAscending {
		t.Fatalf("sort state mismatch: got field=%v asc=%v, want field=%v asc=%v",
			back.SortField, back.SortAscending, s.SortField, s.SortAscending)
	}
}
