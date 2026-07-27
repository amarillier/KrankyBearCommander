package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestIsNotExistMatchesRealReadDirError guards the exact mechanism
// filelist.go's Reload relies on to detect a vanished drive/directory: a
// real os.ReadDir failure on a missing path must satisfy IsNotExist, and an
// unrelated error must not (so a permission-denied/network-timeout error
// still surfaces to the user instead of silently redirecting them home).
func TestIsNotExistMatchesRealReadDirError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := os.ReadDir(missing)
	if err == nil {
		t.Fatalf("expected an error reading %q, got nil", missing)
	}
	if !IsNotExist(err) {
		t.Fatalf("IsNotExist(%v) = false, want true for a missing path", err)
	}

	if IsNotExist(errors.New("some other failure")) {
		t.Fatal("IsNotExist should not match an unrelated generic error")
	}
	if IsNotExist(nil) {
		t.Fatal("IsNotExist(nil) should be false")
	}
}
