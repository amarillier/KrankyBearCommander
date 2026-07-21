package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeleteToTrash exercises Delete(permanent=false) against this machine's
// real platform trash implementation (trash_darwin.go / trash_unix.go /
// trash_windows.go per build tag). It only checks that the source path is
// gone and no error occurred — it does not assert *where* the platform
// trash implementation puts things, since that's OS/desktop-specific.
func TestDeleteToTrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trash-me.txt")
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Delete([]string{path}, false); err != nil {
		t.Fatalf("Delete to trash failed: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("original path should be gone after trashing")
	}
}
