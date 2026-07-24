package zipfs

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"commander/internal/fsops"
)

// buildTestZip creates a zip at dir/name.zip containing files at each of
// the given internal paths (content = the path itself, for easy assertions)
// plus an explicit directory entry for each dir in dirs.
func buildTestZip(t *testing.T, dir, name string, files map[string]string, dirs []string) string {
	t.Helper()
	zipPath := filepath.Join(dir, name)
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, d := range dirs {
		if _, err := zw.Create(d + "/"); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestReadDirRoot(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{
		"top.txt":        "top",
		"sub/nested.txt": "nested",
	}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	entries, err := fs.ReadDir(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d root entries, want 2 (top.txt, sub): %+v", len(entries), entries)
	}
	byName := map[string]bool{}
	for _, e := range entries {
		byName[e.Name] = e.IsDir
	}
	if isDir, ok := byName["sub"]; !ok || !isDir {
		t.Fatalf("expected sub to be a synthesized directory entry, got %+v", byName)
	}
	if isDir, ok := byName["top.txt"]; !ok || isDir {
		t.Fatalf("expected top.txt to be a file entry, got %+v", byName)
	}
}

func TestReadDirSubdirectory(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{
		"sub/nested.txt": "nested",
	}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	entries, err := fs.ReadDir(zipPath + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "nested.txt" {
		t.Fatalf("got %+v, want a single nested.txt entry", entries)
	}
}

func TestOpenReadsContent(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{
		"top.txt": "hello world",
	}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	rc, err := fs.Open(zipPath + "/top.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q, want %q", data, "hello world")
	}
}

func TestDirEscapesArchiveAtRoot(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"top.txt": "top"}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if got, want := fs.Dir(zipPath), dir; got != want {
		t.Fatalf("Dir(root) = %q, want the real containing directory %q", got, want)
	}
}

func TestDirWithinArchiveStaysInside(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"sub/nested.txt": "nested"}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if got, want := fs.Dir(zipPath+"/sub"), zipPath; got != want {
		t.Fatalf("Dir(sub) = %q, want archive root %q", got, want)
	}
}

func TestIsInside(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"sub/nested.txt": "nested"}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if !fs.IsInside(zipPath) {
		t.Fatal("archive's own root should be inside itself")
	}
	if !fs.IsInside(zipPath + "/sub") {
		t.Fatal("a path within the archive should be inside it")
	}
	if fs.IsInside(dir) {
		t.Fatal("the real containing directory should not be inside the archive")
	}
	if fs.IsInside(zipPath + "suffix-that-just-happens-to-share-a-prefix.zip") {
		t.Fatal("a sibling file sharing zipPath as a string prefix should not be considered inside")
	}
}

func TestExtractFile(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"top.txt": "hello"}, nil)
	destDir := t.TempDir()

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if err := fs.Extract([]string{zipPath + "/top.txt"}, destDir, nil, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "top.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("extracted content = %q, want hello", data)
	}
}

func TestExtractDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{
		"proj/top.txt":      "top",
		"proj/sub/deep.txt": "deep",
	}, nil)
	destDir := t.TempDir()

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if err := fs.Extract([]string{zipPath + "/proj"}, destDir, nil, nil); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "proj", "top.txt")); err != nil || string(data) != "top" {
		t.Fatalf("proj/top.txt = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "proj", "sub", "deep.txt")); err != nil || string(data) != "deep" {
		t.Fatalf("proj/sub/deep.txt = %q, %v", data, err)
	}
}

func TestExtractConflictSkip(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"top.txt": "new"}, nil)
	destDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(destDir, "top.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	err = fs.Extract([]string{zipPath + "/top.txt"}, destDir, nil, func(string) (fsops.ConflictAction, string) {
		return fsops.ConflictSkip, ""
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "top.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("skip should leave destination untouched, got %q", data)
	}
}

func TestReadOnlyOperationsFail(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildTestZip(t, dir, "a.zip", map[string]string{"top.txt": "top"}, nil)

	fs, err := Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	if err := fs.Mkdir(zipPath + "/newdir"); err != ErrReadOnly {
		t.Fatalf("Mkdir err = %v, want ErrReadOnly", err)
	}
	if err := fs.Remove(zipPath + "/top.txt"); err != ErrReadOnly {
		t.Fatalf("Remove err = %v, want ErrReadOnly", err)
	}
	if err := fs.Rename(zipPath+"/top.txt", zipPath+"/other.txt"); err != ErrReadOnly {
		t.Fatalf("Rename err = %v, want ErrReadOnly", err)
	}
	if _, err := fs.Create(zipPath + "/newfile.txt"); err != ErrReadOnly {
		t.Fatalf("Create err = %v, want ErrReadOnly", err)
	}
}

func TestHasZipExt(t *testing.T) {
	cases := map[string]bool{
		"archive.zip": true,
		"Archive.ZIP": true,
		"archive.7z":  false,
		"archive":     false,
	}
	for name, want := range cases {
		if got := HasZipExt(name); got != want {
			t.Errorf("HasZipExt(%q) = %v, want %v", name, got, want)
		}
	}
}
