package localfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirAndStat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	fs := New()
	entries, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}

	var gotFile, gotDir bool
	for _, e := range entries {
		switch e.Name {
		case "a.txt":
			gotFile = true
			if e.IsDir || e.Size != 5 {
				t.Fatalf("a.txt entry wrong: %+v", e)
			}
		case "sub":
			gotDir = true
			if !e.IsDir {
				t.Fatalf("sub entry should be a dir: %+v", e)
			}
		}
	}
	if !gotFile || !gotDir {
		t.Fatalf("missing expected entries: %+v", entries)
	}

	st, err := fs.Stat(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Size != 5 || st.IsDir {
		t.Fatalf("Stat wrong: %+v", st)
	}
}

func TestMkdirRenameRemove(t *testing.T) {
	dir := t.TempDir()
	fs := New()

	newDir := filepath.Join(dir, "created")
	if err := fs.Mkdir(newDir); err != nil {
		t.Fatal(err)
	}
	if st, err := fs.Stat(newDir); err != nil || !st.IsDir {
		t.Fatalf("created dir not found or not a dir: %v %+v", err, st)
	}

	renamed := filepath.Join(dir, "renamed")
	if err := fs.Rename(newDir, renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(renamed); err != nil {
		t.Fatalf("renamed dir not found: %v", err)
	}

	if err := fs.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(renamed); err == nil {
		t.Fatal("expected removed dir to be gone")
	}
}

func TestRoots(t *testing.T) {
	roots, err := New().Roots()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 {
		t.Fatal("expected at least one root")
	}
}
