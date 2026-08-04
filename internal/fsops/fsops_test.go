package fsops

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

// TestSetAttributesReadOnly covers the one attribute with a real,
// cross-platform-testable effect (os.Chmod's owner-write bit) — Hidden/
// Archive/System are Windows/macOS-syscall-only (see attrs_windows.go/
// attrs_darwin.go) and can't be meaningfully asserted on whatever platform
// `go test` happens to run on; those are manually verified only.
func TestSetAttributesReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "content")

	if err := SetAttributes([]string{path}, false, Attributes{ReadOnly: AttrSet}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("expected owner-write bit cleared after AttrSet, mode = %v", info.Mode())
	}

	if err := SetAttributes([]string{path}, false, Attributes{ReadOnly: AttrClear}); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("expected owner-write bit restored after AttrClear, mode = %v", info.Mode())
	}
}

func TestSetAttributesUnchangedLeavesReadOnlyAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "content")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := SetAttributes([]string{path}, false, Attributes{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() {
		t.Fatalf("AttrUnchanged (zero value) should leave the mode alone: before %v, after %v", before.Mode(), after.Mode())
	}
}

func TestSetAttributesTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "content")

	want := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := SetAttributes([]string{path}, false, Attributes{SetTime: true, ModTime: want}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(want) {
		t.Fatalf("ModTime = %v, want %v", info.ModTime(), want)
	}
}

func TestSetAttributesRecursesIntoSubdirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "sub", "a.txt")
	mustWriteFile(t, nested, "content")

	if err := SetAttributes([]string{dir}, true, Attributes{ReadOnly: AttrSet}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("expected the nested file's owner-write bit cleared too, mode = %v", info.Mode())
	}
}

// TestSetAttributesReadOnlySkipsDirectories is a regression test: clearing
// a directory's own write bit on POSIX blocks adding/removing/renaming
// anything inside it (including undoing the change again later) — a far
// more disruptive lock than "read-only" means for a folder on Windows.
// SetAttributes must never do this, even when recursing.
func TestSetAttributesReadOnlySkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sub, "a.txt"), "content")

	if err := SetAttributes([]string{dir}, true, Attributes{ReadOnly: AttrSet}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sub)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("directory should keep its write bit, mode = %v", info.Mode())
	}
	// Confirm the tree is still fully usable afterward (this would fail if
	// sub had lost its own write bit).
	if err := os.Remove(filepath.Join(sub, "a.txt")); err != nil {
		t.Fatalf("nested file should still be removable: %v", err)
	}
}

// TestSetAttributesPermissionBits covers the full POSIX owner/group/other
// grid — Set/Clear/Unchanged per bit, independent of ReadOnly's own
// simplified single-bit concept. Windows' os.Chmod only ever respects the
// owner-write bit, so this is skipped there — it's meaningfully tested on
// macOS/Linux only, matching the UI, which only offers this grid there too.
func TestSetAttributesPermissionBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits aren't meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "content")
	if err := os.Chmod(path, 0o644); err != nil { // rw-r--r--
		t.Fatal(err)
	}

	if err := SetAttributes([]string{path}, false, Attributes{
		OwnerExecute: AttrSet,   // add owner execute
		GroupWrite:   AttrSet,   // add group write
		OtherRead:    AttrClear, // drop other read
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Want: rwxrw----  (0o760): owner rwx (r,w unchanged, x added), group
	// rw- (r unchanged, w added, x still unchanged/clear), other --- (r
	// cleared, w/x unchanged/clear).
	if got, want := info.Mode().Perm(), fs.FileMode(0o760); got != want {
		t.Fatalf("mode = %v, want %v", got, want)
	}
}

func TestSetAttributesPermissionBitsUnchangedLeavesModeAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits aren't meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "content")
	if err := os.Chmod(path, 0o741); err != nil {
		t.Fatal(err)
	}

	if err := SetAttributes([]string{path}, false, Attributes{SetTime: true, ModTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), fs.FileMode(0o741); got != want {
		t.Fatalf("mode = %v, want unchanged %v", got, want)
	}
}

// TestSetAttributesPermissionBitsAppliesToDirectoriesToo is the deliberate
// contrast with TestSetAttributesReadOnlySkipsDirectories: someone reaching
// for the full permission grid has already opted into standard chmod-style
// control and should get chmod -R-style behavior, directories included.
func TestSetAttributesPermissionBitsAppliesToDirectoriesToo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits aren't meaningful on Windows")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sub, "a.txt"), "content")

	if err := SetAttributes([]string{dir}, true, Attributes{OtherWrite: AttrSet}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sub)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o002 == 0 {
		t.Fatalf("expected the directory itself to also get other-write set, mode = %v", info.Mode())
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

func TestDuplicateFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "hello")

	dest, err := Duplicate(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "a copy.txt"); dest != want {
		t.Fatalf("dest = %q, want %q", dest, want)
	}
	if got := mustReadFile(t, dest); got != "hello" {
		t.Fatalf("duplicated content = %q, want hello", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("source should still exist after Duplicate: %v", err)
	}
}

func TestDuplicateNameCollisionIncrements(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "first")
	mustWriteFile(t, filepath.Join(dir, "a copy.txt"), "already here")

	dest, err := Duplicate(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "a copy 2.txt"); dest != want {
		t.Fatalf("dest = %q, want %q", dest, want)
	}
}

func TestDuplicateDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proj", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "proj", "sub", "nested.txt"), "nested")

	dest, err := Duplicate(filepath.Join(dir, "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "proj copy"); dest != want {
		t.Fatalf("dest = %q, want %q", dest, want)
	}
	if got := mustReadFile(t, filepath.Join(dest, "sub", "nested.txt")); got != "nested" {
		t.Fatalf("duplicated nested content = %q, want nested", got)
	}
}

func TestSymlink(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "hello")

	link := filepath.Join(dir, "a-link.txt")
	if err := Symlink(filepath.Join(dir, "a.txt"), link); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, link); got != "hello" {
		t.Fatalf("reading through the symlink = %q, want hello", got)
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(dir, "a.txt") {
		t.Fatalf("link target = %q, want %q", target, filepath.Join(dir, "a.txt"))
	}
}

func mustReadZipEntry(t *testing.T, zipPath, name string) string {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	t.Fatalf("zip entry %q not found in %s", name, zipPath)
	return ""
}

func TestCompressFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "hello")
	destZip := filepath.Join(dir, "out.zip")

	if err := Compress([]string{filepath.Join(dir, "a.txt")}, destZip); err != nil {
		t.Fatal(err)
	}
	if got := mustReadZipEntry(t, destZip, "a.txt"); got != "hello" {
		t.Fatalf("zipped content = %q, want hello", got)
	}
}

func TestCompressDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proj", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "proj", "top.txt"), "top")
	mustWriteFile(t, filepath.Join(dir, "proj", "sub", "nested.txt"), "nested")
	destZip := filepath.Join(dir, "out.zip")

	if err := Compress([]string{filepath.Join(dir, "proj")}, destZip); err != nil {
		t.Fatal(err)
	}
	if got := mustReadZipEntry(t, destZip, "proj/top.txt"); got != "top" {
		t.Fatalf("proj/top.txt = %q, want top", got)
	}
	if got := mustReadZipEntry(t, destZip, "proj/sub/nested.txt"); got != "nested" {
		t.Fatalf("proj/sub/nested.txt = %q, want nested", got)
	}
}

func TestCompressNameSingleSource(t *testing.T) {
	dir := t.TempDir()
	got := CompressName(dir, []string{filepath.Join(dir, "report.txt")}, "zip")
	if want := filepath.Join(dir, "report.zip"); got != want {
		t.Fatalf("CompressName = %q, want %q", got, want)
	}
}

func TestCompressNameMultipleSourcesAndCollision(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "Archive.zip"), "existing")

	got := CompressName(dir, []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}, "zip")
	if want := filepath.Join(dir, "Archive 2.zip"); got != want {
		t.Fatalf("CompressName = %q, want %q", got, want)
	}
}

func TestSymlinkNameDefault(t *testing.T) {
	dir := t.TempDir()
	got := SymlinkName(dir, "photo.jpg")
	if want := filepath.Join(dir, "link-photo.jpg"); got != want {
		t.Fatalf("SymlinkName = %q, want %q", got, want)
	}
}

func TestSymlinkNameCollisionIncrements(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "link-photo.jpg"), "existing")

	got := SymlinkName(dir, "photo.jpg")
	if want := filepath.Join(dir, "link-photo 2.jpg"); got != want {
		t.Fatalf("SymlinkName = %q, want %q", got, want)
	}
}

func TestSevenZipAvailableOverrideMissing(t *testing.T) {
	if _, ok := SevenZipAvailable(filepath.Join(t.TempDir(), "no-such-binary")); ok {
		t.Fatal("expected a nonexistent override path to report unavailable")
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "top.txt"), "12345") // 5 bytes
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "sub", "nested.txt"), "1234567") // 7 bytes

	got, err := DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 12 {
		t.Fatalf("DirSize = %d, want 12 (5 + 7 bytes, recursive)", got)
	}
}
