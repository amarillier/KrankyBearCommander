package fsops

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

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCopyFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")

	if err := Copy([]string{filepath.Join(src, "a.txt")}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "hello" {
		t.Fatalf("copied content = %q, want hello", got)
	}
	// Source should be untouched by Copy.
	if _, err := os.Stat(filepath.Join(src, "a.txt")); err != nil {
		t.Fatalf("source should still exist after Copy: %v", err)
	}
}

func TestCopyDirectoryRecursive(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "proj", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(src, "proj", "top.txt"), "top")
	mustWriteFile(t, filepath.Join(src, "proj", "sub", "nested.txt"), "nested")

	if err := Copy([]string{filepath.Join(src, "proj")}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "proj", "top.txt")); got != "top" {
		t.Fatalf("top.txt = %q", got)
	}
	if got := mustReadFile(t, filepath.Join(dst, "proj", "sub", "nested.txt")); got != "nested" {
		t.Fatalf("nested.txt = %q", got)
	}
}

func TestCopyConflictSkip(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "a.txt"), "old")

	err := Copy([]string{filepath.Join(src, "a.txt")}, dst, nil, func(string) (ConflictAction, string) {
		return ConflictSkip, ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "old" {
		t.Fatalf("skip should leave destination untouched, got %q", got)
	}
}

func TestCopyConflictOverwrite(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "a.txt"), "old")

	err := Copy([]string{filepath.Join(src, "a.txt")}, dst, nil, func(string) (ConflictAction, string) {
		return ConflictOverwrite, ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "new" {
		t.Fatalf("overwrite should replace destination, got %q", got)
	}
}

func TestCopyConflictRename(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "a.txt"), "old")

	err := Copy([]string{filepath.Join(src, "a.txt")}, dst, nil, func(string) (ConflictAction, string) {
		return ConflictRename, "a (2).txt"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "old" {
		t.Fatalf("original destination should be untouched, got %q", got)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a (2).txt")); got != "new" {
		t.Fatalf("renamed copy = %q, want new", got)
	}
}

func TestCopyConflictCancel(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "a.txt"), "old")

	err := Copy([]string{filepath.Join(src, "a.txt")}, dst, nil, func(string) (ConflictAction, string) {
		return ConflictCancel, ""
	})
	if err != ErrCancelled {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestMoveWithinSameFilesystem(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")

	if err := Move([]string{filepath.Join(src, "a.txt")}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "a.txt")); err == nil {
		t.Fatal("source should be gone after Move")
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "hello" {
		t.Fatalf("moved content = %q, want hello", got)
	}
}

func TestMoveDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(src, "proj", "f.txt"), "data")

	if err := Move([]string{filepath.Join(src, "proj")}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "proj")); err == nil {
		t.Fatal("source dir should be gone after Move")
	}
	if got := mustReadFile(t, filepath.Join(dst, "proj", "f.txt")); got != "data" {
		t.Fatalf("moved nested content = %q, want data", got)
	}
}

func TestMoveConflictSkip(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	os.MkdirAll(src, 0o755)
	os.MkdirAll(dst, 0o755)
	mustWriteFile(t, filepath.Join(src, "a.txt"), "new")
	mustWriteFile(t, filepath.Join(dst, "a.txt"), "old")

	err := Move([]string{filepath.Join(src, "a.txt")}, dst, nil, func(string) (ConflictAction, string) {
		return ConflictSkip, ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(src, "a.txt")); err != nil {
		t.Fatal("skipped source file should remain in place")
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "old" {
		t.Fatalf("destination should be untouched, got %q", got)
	}
}

func TestDeletePermanent(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "gone soon")

	if err := Delete([]string{filepath.Join(dir, "a.txt")}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatal("permanently deleted file should be gone")
	}
}

func TestRename(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "old.txt"), "content")

	if err := Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); err == nil {
		t.Fatal("old name should no longer exist")
	}
	if got := mustReadFile(t, filepath.Join(dir, "new.txt")); got != "content" {
		t.Fatalf("renamed content = %q, want content", got)
	}
}

func TestMkdirNested(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")

	if err := Mkdir(target); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(target)
	if err != nil || !st.IsDir() {
		t.Fatalf("expected nested dir to exist: %v %+v", err, st)
	}
}

func TestProgressReportsCumulativeBytes(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "12345")

	var lastDone, lastTotal int64
	var calls int
	err := Copy([]string{filepath.Join(src, "a.txt")}, dst, func(done, total int64, _ string) {
		calls++
		lastDone, lastTotal = done, total
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("expected at least one progress callback")
	}
	if lastDone != 5 || lastTotal != 5 {
		t.Fatalf("final progress = %d/%d, want 5/5", lastDone, lastTotal)
	}
}
