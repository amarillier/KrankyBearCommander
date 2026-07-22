package favorites

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "favorites.json")
	want := List{Entries: []Entry{
		{Label: "Desktop", Path: "/home/user/Desktop"},
		{Label: "Projects", Path: "/home/user/Projects"},
	}}

	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Entries[1].Label != "Projects" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadMissingFileReturnsEmptyList(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("expected empty list, got %+v", got)
	}
}

func TestAddDeduplicates(t *testing.T) {
	var l List
	l.Add("Projects", "/home/user/Projects")
	l.Add("Projects (dup)", "/home/user/Projects")

	if len(l.Entries) != 1 {
		t.Fatalf("expected duplicate path to be ignored, got %+v", l.Entries)
	}
	if !l.Has("/home/user/Projects") {
		t.Fatal("expected Has to report the added path")
	}
}

func TestRemove(t *testing.T) {
	var l List
	l.Add("Desktop", "/home/user/Desktop")
	l.Add("Downloads", "/home/user/Downloads")

	l.Remove("/home/user/Desktop")

	if len(l.Entries) != 1 || l.Entries[0].Path != "/home/user/Downloads" {
		t.Fatalf("expected only Downloads to remain, got %+v", l.Entries)
	}
	if l.Has("/home/user/Desktop") {
		t.Fatal("expected Desktop to be gone after Remove")
	}
}

func TestDefaultSeedCandidatesNonEmpty(t *testing.T) {
	entries := DefaultSeedCandidates("/home/user")
	if len(entries) == 0 {
		t.Fatal("expected at least one seed candidate")
	}
	for _, e := range entries {
		if e.Label == "" || e.Path == "" {
			t.Fatalf("seed entry missing label/path: %+v", e)
		}
	}
	if runtime.GOOS == "darwin" {
		found := false
		for _, e := range entries {
			if e.Path == "/Applications" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected /Applications in macOS seed candidates")
		}
	}
}
