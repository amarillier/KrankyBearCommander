package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindDuplicatesBasic(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "same content")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "same content")
	mustWriteFile(t, filepath.Join(root, "unique.txt"), "nothing else matches this")

	groups, err := FindDuplicates(root, true, -1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if len(groups[0].Paths) != 2 {
		t.Fatalf("group members = %d, want 2", len(groups[0].Paths))
	}
}

func TestFindDuplicatesSameSizeDifferentContentNotGrouped(t *testing.T) {
	root := t.TempDir()
	// Same length (5 bytes each), different content — must NOT be reported
	// as duplicates; only the size pre-filter should treat them alike.
	mustWriteFile(t, filepath.Join(root, "a.txt"), "aaaaa")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "bbbbb")

	groups, err := FindDuplicates(root, true, -1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %d, want 0 (same size, different content)", len(groups))
	}
}

func TestFindDuplicatesExcludesEmptyFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "")

	groups, err := FindDuplicates(root, true, -1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %d, want 0 (empty files must be excluded)", len(groups))
	}
}

func TestFindDuplicatesRecursesSubdirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "nested match")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sub, "b.txt"), "nested match")

	groups, err := FindDuplicates(root, true, -1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Paths) != 2 {
		t.Fatalf("groups = %+v, want one group of 2", groups)
	}
}

func TestFindDuplicatesMaxDepthExcludesDeeperSubdirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "nested match")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sub, "b.txt"), "nested match")

	groups, err := FindDuplicates(root, true, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("groups = %+v, want 0 (maxDepth=0 must not descend into sub)", groups)
	}
}

func TestFindDuplicatesCancellation(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "same content")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "same content")

	_, err := FindDuplicates(root, true, -1, func(phase, path string) bool { return false })
	if err != ErrCancelled {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}
