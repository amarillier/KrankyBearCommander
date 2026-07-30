package listboxfs

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadDirListsGivenEntriesFromWhereverTheyReallyAre(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	mustWriteFile(t, filepath.Join(dirA, "one.txt"), "one")
	mustWriteFile(t, filepath.Join(dirB, "two.txt"), "two")

	root := "Listbox: test"
	fs := New(root, map[string]string{
		"one.txt": filepath.Join(dirA, "one.txt"),
		"two.txt": filepath.Join(dirB, "two.txt"),
	})

	entries, err := fs.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	byName := map[string]int64{}
	for _, e := range entries {
		byName[e.Name] = e.Size
	}
	if byName["one.txt"] != 3 || byName["two.txt"] != 3 {
		t.Fatalf("entry sizes = %+v, want one.txt=3 two.txt=3", byName)
	}
}

func TestReadDirSkipsVanishedEntries(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "stays.txt"), "x")

	root := "Listbox: test"
	fs := New(root, map[string]string{
		"stays.txt": filepath.Join(dir, "stays.txt"),
		"gone.txt":  filepath.Join(dir, "gone.txt"), // never created
	})

	entries, err := fs.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "stays.txt" {
		t.Fatalf("got %+v, want only stays.txt", entries)
	}
}

func TestReadDirRejectsNonRootPath(t *testing.T) {
	fs := New("Listbox: test", map[string]string{})
	if _, err := fs.ReadDir("/some/other/path"); err == nil {
		t.Fatal("expected an error reading a path other than the listbox's own root")
	}
}

func TestJoinResolvesToRealPath(t *testing.T) {
	real := filepath.Join(t.TempDir(), "file.txt")
	root := "Listbox: test"
	fs := New(root, map[string]string{"file.txt": real})

	if got := fs.Join(root, "file.txt"); got != real {
		t.Fatalf("Join(root, name) = %q, want the real path %q", got, real)
	}
}

func TestJoinUnknownNameDegradesGracefully(t *testing.T) {
	root := "Listbox: test"
	fs := New(root, map[string]string{})

	got := fs.Join(root, "never-added.txt")
	if got == "" {
		t.Fatal("Join for an unrecognized name should still return something usable, not empty")
	}
}

func TestJoinPassesThroughNonRootPaths(t *testing.T) {
	fs := New("Listbox: test", map[string]string{})
	want := filepath.Join("a", "b", "c")
	if got := fs.Join("a", "b", "c"); got != want {
		t.Fatalf("Join(a,b,c) = %q, want plain filepath.Join result %q", got, want)
	}
}

func TestDirOfRootIsRootItself(t *testing.T) {
	root := "Listbox: test"
	fs := New(root, map[string]string{})
	if got := fs.Dir(root); got != root {
		t.Fatalf("Dir(root) = %q, want root %q unchanged (no parent to walk up into)", got, root)
	}
}

func TestDirOfRealPathIsOrdinary(t *testing.T) {
	fs := New("Listbox: test", map[string]string{})
	real := filepath.Join("some", "dir", "file.txt")
	if got, want := fs.Dir(real), filepath.Dir(real); got != want {
		t.Fatalf("Dir(%q) = %q, want %q", real, got, want)
	}
}

func TestRenameActuallyMovesTheRealFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")
	mustWriteFile(t, oldPath, "content")

	fs := New("Listbox: test", map[string]string{"old.txt": oldPath})
	if err := fs.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new.txt should exist after rename: %v", err)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatal("old.txt should no longer exist after rename")
	}
}

func TestRemoveActuallyDeletesTheRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doomed.txt")
	mustWriteFile(t, path, "x")

	fs := New("Listbox: test", map[string]string{"doomed.txt": path})
	if err := fs.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("doomed.txt should no longer exist after Remove")
	}
}

func TestStatOfRootIsADirectory(t *testing.T) {
	root := "Listbox: test"
	fs := New(root, map[string]string{})
	entry, err := fs.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.IsDir {
		t.Fatal("Stat(root) should report a directory")
	}
}

func TestIsInsideOnlyTrueForRootItself(t *testing.T) {
	root := "Listbox: test"
	fs := New(root, map[string]string{})
	if !fs.IsInside(root) {
		t.Fatal("the listing's own root should be inside itself")
	}
	if fs.IsInside("/some/real/path") {
		t.Fatal("a real path should never be considered inside a listbox view")
	}
}

func TestCloseIsANoop(t *testing.T) {
	fs := New("Listbox: test", map[string]string{})
	if err := fs.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
