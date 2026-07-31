package main

import (
	"os"
	"path/filepath"
	"testing"

	"commander/internal/vfs/localfs"
)

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyMultiRenameSimple(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "a.txt"), "A")
	mustWriteTestFile(t, filepath.Join(dir, "b.txt"), "B")

	err := applyMultiRename(localfs.New(), dir, []string{"a.txt", "b.txt"}, []string{"x.txt", "y.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "x.txt")); got != "A" {
		t.Fatalf("x.txt = %q, want A", got)
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "y.txt")); got != "B" {
		t.Fatalf("y.txt = %q, want B", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err == nil {
		t.Fatal("a.txt should no longer exist")
	}
}

// TestApplyMultiRenameSwap is the case that needs the two-phase temp-name
// rename: renaming a.txt->b.txt and b.txt->a.txt in the same batch would
// clobber b.txt with a naive sequential rename.
func TestApplyMultiRenameSwap(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "a.txt"), "A")
	mustWriteTestFile(t, filepath.Join(dir, "b.txt"), "B")

	err := applyMultiRename(localfs.New(), dir, []string{"a.txt", "b.txt"}, []string{"b.txt", "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "a.txt")); got != "B" {
		t.Fatalf("a.txt = %q, want B (swapped)", got)
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "b.txt")); got != "A" {
		t.Fatalf("b.txt = %q, want A (swapped)", got)
	}
}

func TestApplyMultiRenameNoChangeIsNoop(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "a.txt"), "A")

	err := applyMultiRename(localfs.New(), dir, []string{"a.txt"}, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "a.txt")); got != "A" {
		t.Fatalf("a.txt = %q, want A (untouched)", got)
	}
}

func TestApplyMultiRenameRejectsDuplicateResults(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "a.txt"), "A")
	mustWriteTestFile(t, filepath.Join(dir, "b.txt"), "B")

	err := applyMultiRename(localfs.New(), dir, []string{"a.txt", "b.txt"}, []string{"same.txt", "same.txt"})
	if err == nil {
		t.Fatal("expected an error for two items renaming to the same result")
	}
	// Neither should have moved.
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal("a.txt should be untouched after a rejected batch")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("b.txt should be untouched after a rejected batch")
	}
}

func TestApplyMultiRenameRejectsCollisionWithUnrelatedFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "a.txt"), "A")
	mustWriteTestFile(t, filepath.Join(dir, "existing.txt"), "existing")

	err := applyMultiRename(localfs.New(), dir, []string{"a.txt"}, []string{"existing.txt"})
	if err == nil {
		t.Fatal("expected an error when the new name collides with an unrelated existing file")
	}
	if got := mustReadTestFile(t, filepath.Join(dir, "existing.txt")); got != "existing" {
		t.Fatalf("existing.txt should be untouched, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal("a.txt should be untouched after a rejected batch")
	}
}

// TestApplyMultiRenameCaseOnlyRenameOfSelf is a regression test for a real
// false "already exists" report: renaming a file to a different case of
// its own name (e.g. fixing "80s" wrongly title-cased to "80S") must
// succeed, not be mistaken for colliding with an unrelated existing file —
// macOS/Windows filesystems are case-insensitive by default, so the old
// and new names name the same file.
func TestApplyMultiRenameCaseOnlyRenameOfSelf(t *testing.T) {
	dir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(dir, "80s rock.mp3"), "content")

	err := applyMultiRename(localfs.New(), dir, []string{"80s rock.mp3"}, []string{"80S rock.mp3"})
	if err != nil {
		t.Fatal(err)
	}

	// Read back the actual directory entry name rather than just opening
	// "80S rock.mp3" by path — on a case-insensitive filesystem that alone
	// wouldn't distinguish "really renamed" from "silently did nothing,
	// but the case-insensitive lookup found the old name anyway."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "80S rock.mp3" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory entries = %v, want exactly [80S rock.mp3]", names)
	}
}
