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

func TestRunLaunchesCommandWithNoArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shebang script")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	// Simulates an Application Launcher entry — no arguments appended,
	// unlike OpenWith.
	script := filepath.Join(dir, "app.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \""+marker+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Run(script, RunOptions{}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Run's command never ran (marker file was not created)")
}

func TestRunPassesArgsWorkingDirAndEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shebang script")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	workDir := t.TempDir()
	// Writes: arg1, cwd, and $FOO — one per line — so the test can confirm
	// Args/WorkingDir/Env all actually reached the child process.
	script := filepath.Join(dir, "app.sh")
	content := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$1\" \"$(pwd)\" \"$FOO\" > \"" + marker + "\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := RunOptions{Args: []string{"hello"}, WorkingDir: workDir, Env: []string{"FOO=bar"}}
	if err := Run(script, opts); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(marker)
		if err == nil {
			got := string(b)
			// Resolve symlinks (e.g. macOS's /tmp -> /private/tmp) so the
			// comparison isn't thrown off by a path the shell reports
			// differently than the one we passed in.
			wantWorkDir, _ := filepath.EvalSymlinks(workDir)
			want := "hello\n" + wantWorkDir + "\nbar\n"
			if got != want {
				t.Fatalf("marker content = %q, want %q", got, want)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Run's command never ran (marker file was not created)")
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"--flag", []string{"--flag"}},
		{"--title \"My App\"", []string{"--title", "My App"}},
		{"--title 'My App' --flag", []string{"--title", "My App", "--flag"}},
		{"a b   c", []string{"a", "b", "c"}},
		{`"quoted"unquoted`, []string{"quotedunquoted"}},
	}
	for _, c := range cases {
		got := SplitArgs(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("SplitArgs(%q) = %#v, want %#v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("SplitArgs(%q) = %#v, want %#v", c.in, got, c.want)
			}
		}
	}
}

func TestOpenWithRunsCommandWithPathArgument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shebang script")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "editor.sh")
	// Simulates an external editor invoked as `editor.sh <file>`.
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := OpenWith(script, marker); err != nil {
		t.Fatalf("OpenWith failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("OpenWith's command never ran (marker file was not created)")
}
