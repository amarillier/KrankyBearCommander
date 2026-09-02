package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileKnownDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "hello")

	// sha256("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	got, err := HashFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("HashFile = %q, want %q", got, want)
	}
}

func TestHashFileSameContentSameHash(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	mustWriteFile(t, a, "identical content")
	mustWriteFile(t, b, "identical content")

	ha, err := HashFile(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := HashFile(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("identical files hashed differently: %q vs %q", ha, hb)
	}
}

func TestHashFileCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	mustWriteFile(t, path, "hello world")

	_, err := HashFile(path, func(int64) bool { return false })
	if err != ErrCancelled {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestCopyVerifyHappyPath(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")

	if err := CopyVerify([]string{filepath.Join(src, "a.txt")}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "hello" {
		t.Fatalf("copied content = %q, want hello", got)
	}
}

func TestMoveVerifyHappyPath(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	srcPath := filepath.Join(src, "a.txt")
	mustWriteFile(t, srcPath, "hello")

	if err := MoveVerify([]string{srcPath}, dst, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "hello" {
		t.Fatalf("moved content = %q, want hello", got)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("source should be gone after a successful MoveVerify, stat err = %v", err)
	}
}

// TestCopyVerifyMismatchFails simulates a destination that always fails
// verification (e.g. persistent write-path corruption) via the
// hashFileForVerify test seam — CopyVerify should retry retryAttempts times
// and then fail with a *VerifyError, and must NOT silently accept the bad
// copy.
func TestCopyVerifyMismatchFails(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")

	orig := hashFileForVerify
	var calls int
	hashFileForVerify = func(path string, progress func(int64) bool) (string, error) {
		calls++
		return "always-different-from-source-hash", nil
	}
	defer func() { hashFileForVerify = orig }()

	err := CopyVerify([]string{filepath.Join(src, "a.txt")}, dst, nil, nil)
	if err == nil {
		t.Fatal("expected a verification error, got nil")
	}
	verr, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("err = %#v (%T), want *VerifyError", err, err)
	}
	if calls != retryAttempts {
		t.Fatalf("hashFileForVerify called %d times, want %d (retryAttempts)", calls, retryAttempts)
	}
	_ = verr
}

// Note: there's no MoveVerify-mismatch-keeps-source test here — t.TempDir()
// directories normally share a filesystem, so MoveVerify takes moveAll's
// atomic os.Rename fast path (correctly: no bytes are rewritten, so there's
// nothing to verify) rather than the copy+verify+delete fallback, and that
// fallback isn't portably forceable in a unit test. The guarantee itself
// (removeAllWithProgress only runs after copyPath returns success) is
// structurally identical to what TestCopyVerifyMismatchFails already proves
// for copyFile/copyFileOnce/verifyDestHash, which both Copy and Move share.

// TestCopyVerifyRecoversAfterRetry confirms a transient mismatch (the first
// attempt "fails," a later one "succeeds") is NOT surfaced as an error —
// this is the scenario the retry loop exists for.
func TestCopyVerifyRecoversAfterRetry(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "a.txt"), "hello")

	orig := hashFileForVerify
	var calls int
	hashFileForVerify = func(path string, progress func(int64) bool) (string, error) {
		calls++
		if calls < retryAttempts {
			return "always-different-from-source-hash", nil
		}
		return HashFile(path, progress)
	}
	defer func() { hashFileForVerify = orig }()

	if err := CopyVerify([]string{filepath.Join(src, "a.txt")}, dst, nil, nil); err != nil {
		t.Fatalf("expected recovery on the final attempt, got error: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(dst, "a.txt")); got != "hello" {
		t.Fatalf("copied content = %q, want hello", got)
	}
}
