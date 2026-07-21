package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit detection is Unix-specific; Windows uses extension matching")
	}

	dir := t.TempDir()
	exePath := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plainPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plainPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsExecutable(exePath) {
		t.Error("expected a script with the executable bit set to be executable")
	}
	if IsExecutable(plainPath) {
		t.Error("expected a plain file without the executable bit to not be executable")
	}
	if IsExecutable(dir) {
		t.Error("a directory should never be treated as executable")
	}
}

func TestOpenSpawnsExecutableDetached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shebang script")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "run.sh")
	content := "#!/bin/sh\ntouch \"" + marker + "\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Open(script); err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("spawned script never ran (marker file was not created)")
}
