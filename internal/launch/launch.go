// Package launch opens a file with the OS's default association, EXCEPT for
// files with the executable bit set (or a .exe/.bat/.cmd extension on
// Windows), which are spawned directly as detached processes instead.
//
// Two problems this avoids: (1) the OS "open" mechanism only knows document
// associations, not "run this" — on macOS, `open` on a bare executable/script
// actually launches it inside a new Terminal.app window rather than running
// it directly, which is not what a double-click on a program should do; (2)
// a plain os/exec child inherits our process group, so if we don't detach it
// explicitly, closing/signalling the file manager's process group can take
// the launched program down with it — the whole point of "launch" is that it
// keeps running after the file manager quits.
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// IsExecutable reports whether path should be spawned directly rather than
// handed to the OS's default file association.
func IsExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd":
			return true
		default:
			return false
		}
	}
	return info.Mode().Perm()&0o111 != 0
}

// Open launches path: directly and detached if it's executable, otherwise
// via the OS's default file association.
func Open(path string) error {
	if IsExecutable(path) {
		return spawnDetached(path)
	}
	return openWithDefaultApp(path)
}

// spawnDetached runs path with detachAttr (see launch_unix.go /
// launch_windows.go) so it survives this process exiting and, on macOS,
// doesn't get wrapped in a Terminal.app window the way `open` would.
func spawnDetached(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	cmd.SysProcAttr = detachAttr()
	return cmd.Start()
}

// OpenWith runs `command path` detached the same way Open's direct-execute
// path does — for external-editor / "open with a specific program"
// integrations, as opposed to Open's own executable-vs-file-association
// choice.
func OpenWith(command, path string) error {
	cmd := exec.Command(command, path)
	cmd.SysProcAttr = detachAttr()
	return cmd.Start()
}

// RevealInFileManager opens the OS's native file manager (Finder, Explorer,
// or the desktop's default file manager on Linux) with path highlighted —
// "Reveal in File Manager" in the right-click context menu. macOS and
// Windows both support selecting the specific item within its parent
// folder; Linux has no universal "select this file" command, so isDir
// decides whether to open the item itself (a directory) or its parent (a
// file) instead.
func RevealInFileManager(path string, isDir bool) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	default:
		target := path
		if !isDir {
			target = filepath.Dir(path)
		}
		return exec.Command("xdg-open", target).Start()
	}
}

// ShowProperties opens the OS's native file-properties dialog for path —
// Windows Explorer's Properties, macOS Finder's Get Info. There's no
// equivalent universal command on Linux, so callers should simply not
// offer this there (see contextmenu_ui.go, which only adds the menu item
// on darwin/windows, mirroring how "To .7z" only appears when a 7z binary
// is actually available).
func ShowProperties(path string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`tell application "Finder" to open information window of (POSIX file %q as alias)`, path)
		return exec.Command("osascript", "-e", script).Start()
	case "windows":
		return showPropertiesWindows(path)
	default:
		return errors.New("viewing file properties isn't supported on this platform")
	}
}

// RunOptions configures Run — all fields optional/zero-value-safe.
type RunOptions struct {
	// Args are passed as-is (already split — see SplitArgs for turning a
	// user-typed "Parameters" string into this).
	Args []string
	// WorkingDir is the directory the process starts in ("Start In") — the
	// launched process's own cwd if left empty, i.e. Commander's.
	WorkingDir string
	// Env is "KEY=VALUE" pairs APPENDED to the inherited environment
	// (augmenting it, not replacing it) — a later duplicate key wins, per
	// os/exec's own documented behavior for a repeated Env entry.
	Env []string
}

// Run launches command directly, detached the same way Open/OpenWith are —
// the Application Launcher's own "click to launch" action. Unlike
// OpenWith, no file-path argument is appended: a launcher entry has
// nothing to open, only whatever opts.Args says to pass.
func Run(command string, opts RunOptions) error {
	cmd := exec.Command(command, opts.Args...)
	cmd.SysProcAttr = detachAttr()
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}
	return cmd.Start()
}

// SplitArgs splits a user-typed "Parameters" string into individual
// arguments, whitespace-separated but respecting "double" and 'single'
// quoted segments so e.g. `--title "My App"` stays one argument rather than
// splitting on the space inside it. A minimal shell-word-splitter, not a
// full shell parser (no variable expansion, escaping is limited to closing
// a quote) — enough for the common case without a new dependency.
func SplitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false

	flush := func() {
		if inArg {
			args = append(args, cur.String())
			cur.Reset()
			inArg = false
		}
	}

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			inArg = true
			cur.WriteRune(r)
		}
	}
	flush()
	return args
}

func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
