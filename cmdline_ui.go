// cmdline_ui.go — the optional bottom command-line bar: type `cd ...` (or a
// bare path) to navigate the active tab itself, or anything else to run as
// a real shell command against that tab's directory. cwd tracking
// deliberately piggybacks on the app's own existing navigation/focus
// callbacks (pane.refreshChrome's onChromeChanged hook, commander.
// setActivePane) rather than tracking a spawned shell process's cwd — see
// dispatchCommand's doc comment for the reasoning Allan walked through
// (TotalCmd's own command line works the same way).
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/launch"
	"commander/internal/vfs"
)

// prefShowCmdLine persists the command-line bar's visibility, same pattern
// as prefShowDriveBar — defaults to shown (true) so the new bar is
// discoverable on first run rather than needing to be found via a menu
// first.
const prefShowCmdLine = "showCmdLine"

// commandGraceTimeout is how long dispatchCommand waits for a captured
// shell command to finish before giving up on showing its output — see
// runCapturedCommand's doc comment.
const commandGraceTimeout = 2 * time.Second

// interactiveShellNames are shell binaries that need a real, visible
// terminal window rather than captured output — piping their stdio into a
// hidden buffer would give an unusable "interactive" shell with no visible
// prompt and no way to type into it after launch.
var interactiveShellNames = map[string]bool{
	"cmd": true, "powershell": true, "pwsh": true,
	"bash": true, "zsh": true, "sh": true, "fish": true,
}

// buildCmdLineBar builds the command-line row (a hidden-until-needed output
// pane, then the cwd label + entry) and returns it for commander.go's
// bottom layout — the whole row is shown/hidden by toggleShowCmdLine.
func (c *commander) buildCmdLineBar() fyne.CanvasObject {
	c.cwdLabel = widget.NewLabel("")

	c.cmdOutputLabel = widget.NewLabel("")
	c.cmdOutputLabel.Wrapping = fyne.TextWrapWord
	c.cmdOutputScroll = container.NewScroll(c.cmdOutputLabel)
	c.cmdOutputScroll.SetMinSize(fyne.NewSize(0, 120))
	c.cmdOutputScroll.Hide()

	c.cmdEntry = newCmdEntry(func(text string) { c.dispatchCommand(text) }, c.hideCmdOutput)

	// Escape only dismisses the output pane while the entry itself has
	// focus — real-usage testing found that clicking into the output to
	// select/copy text (a reasonable thing to want to do) then leaves no
	// way to dismiss it short of clicking back into the entry first. These
	// two buttons give an explicit, always-available mechanism instead of
	// relying on focus/Escape at all.
	copyBtn := ttwidget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() { c.copyCmdOutput() })
	copyBtn.SetToolTip("Copy the command output to the clipboard")
	closeBtn := ttwidget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		c.hideCmdOutput()
		c.win.Canvas().Focus(c.cmdEntry)
	})
	closeBtn.SetToolTip("Close the command output")

	entryRow := container.NewBorder(nil, nil, c.cwdLabel, container.NewHBox(copyBtn, closeBtn), c.cmdEntry)
	c.cmdLineRow = container.NewVBox(c.cmdOutputScroll, entryRow)
	c.refreshCmdLineVisibility()
	return c.cmdLineRow
}

// copyCmdOutput copies the command output pane's current text to the OS
// clipboard — the same c.win.Clipboard().SetContent(...) call
// contextmenu_ui.go's "Copy Name"/"Copy Path" already use.
func (c *commander) copyCmdOutput() {
	c.win.Clipboard().SetContent(c.cmdOutputLabel.Text)
}

// refreshCmdLineVisibility shows or hides the whole command-line row per
// the shared showCmdLine setting — called once at construction and again
// whenever toggleShowCmdLine flips it.
func (c *commander) refreshCmdLineVisibility() {
	if c.cmdLineRow == nil {
		return
	}
	if c.showCmdLine {
		c.cmdLineRow.Show()
	} else {
		c.cmdLineRow.Hide()
	}
}

// refreshCmdLineCwd keeps the command bar's directory label in sync with
// the active pane's active tab — called whenever the active pane changes
// (commander.setActivePane) or a tab navigates (pane.refreshChrome, via the
// onChromeChanged hook), so it never needs its own navigation tracking.
func (c *commander) refreshCmdLineCwd() {
	if c.cwdLabel == nil {
		return
	}
	state := c.activePane().activeState()
	if state == nil {
		c.cwdLabel.SetText("")
		return
	}
	c.cwdLabel.SetText(state.Path + " >")
}

func (c *commander) hideCmdOutput() {
	c.cmdOutputLabel.SetText("")
	c.cmdOutputScroll.Hide()
}

func (c *commander) showCmdOutput(text string) {
	c.cmdOutputLabel.SetText(text)
	c.cmdOutputScroll.Show()
}

// cmdEntry extends widget.Entry the same way rename_ui.go's renameEntry
// does — Fyne's Entry doesn't expose an Escape hook publicly, and per
// CLAUDE.md's key-routing note a focused widget swallows canvas-level key
// handling entirely, so Escape has to be handled here or not at all.
// Escape both clears the entry AND dismisses the output pane (onEscape) —
// otherwise a command that printed output has no way to be dismissed short
// of running another, silent one, which real-usage testing showed reads as
// a bug ("my panes look weird" / reaching for /dev/null to reset it).
type cmdEntry struct {
	widget.Entry
	onEscape func()
}

func newCmdEntry(onSubmit func(string), onEscape func()) *cmdEntry {
	e := &cmdEntry{onEscape: onEscape}
	e.ExtendBaseWidget(e)
	e.OnSubmitted = func(text string) {
		onSubmit(text)
		e.SetText("")
	}
	return e
}

func (e *cmdEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		e.SetText("")
		if e.onEscape != nil {
			e.onEscape()
		}
		return
	}
	e.Entry.TypedKey(ev)
}

// dispatchCommand runs whatever the user submitted in the command bar
// against the currently active pane's active tab, in this order: `cd`-style
// navigation (which actually navigates the tab, not a hidden background
// shell — this is the behavior Allan described from TotalCmd), then a
// refusal if the tab isn't backed by a real local filesystem, then a named
// interactive shell (its own real terminal window), then anything else as
// a captured shell command.
func (c *commander) dispatchCommand(raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}

	p := c.activePane()
	v := p.activeView()
	if v == nil {
		return
	}

	if arg, ok := cdArg(trimmed); ok {
		navigateCmd(p, v, arg)
		return
	}

	if _, remote := v.fs.(remoteConnFS); remote {
		c.showStatus("shell commands need a local tab")
		return
	}

	fields := strings.Fields(trimmed)
	if len(fields) > 0 && isInteractiveShellCommand(fields[0]) {
		if err := launchInteractiveShell(fields[0], v.state.Path); err != nil {
			c.showStatus("couldn't open a terminal: " + err.Error())
		}
		return
	}

	c.runCapturedCommand(trimmed, v)
}

// cdArg reports whether trimmed is a `cd` command and, if so, its (possibly
// empty) argument — matching exactly how Allan typed it (`cd \`), rather
// than also treating a bare path with no `cd` prefix as navigation, to keep
// the classification simple and unambiguous.
func cdArg(trimmed string) (string, bool) {
	fields := strings.Fields(trimmed)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "cd") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0])), true
}

// navigateCmd resolves arg the way a shell's `cd` would and navigates the
// tab itself — not a background shell's cwd — reusing the exact
// Home/lock-aware entry points the Home button and double-click navigation
// already use (filelist.go's Home/navigateTo), so locked-tab behavior and
// connection-tab navigation come for free rather than being reimplemented.
func navigateCmd(p *pane, v *fileListView, arg string) {
	switch {
	case arg == "" || arg == "~":
		v.Home(p.defaultHome())
	case arg == "..":
		v.navigateTo(v.fs.Dir(v.state.Path))
	case arg == "\\" || arg == "/":
		v.navigateTo(rootOf(v.fs, v.state.Path))
	case isAbsolutePath(arg):
		v.navigateTo(arg)
	default:
		v.navigateTo(v.fs.Join(v.state.Path, arg))
	}
}

// rootOf walks path up to its own filesystem root — a Windows drive root or
// "/" — by repeatedly asking fs.Dir, which is idempotent once at the root
// (filepath.Dir("/") == "/", filepath.Dir(`C:\`) == `C:\`). Generic across
// every vfs.FileSystem backend rather than special-casing Windows drive
// letters, and bounded so a misbehaving backend can't loop forever.
func rootOf(fs vfs.FileSystem, path string) string {
	for i := 0; i < 64; i++ {
		parent := fs.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
	return path
}

// isAbsolutePath reports whether arg already names a full path, so it
// should be handed to navigateTo as-is rather than joined onto the current
// directory — filepath.Join doesn't special-case an absolute second
// argument, so this has to be checked explicitly.
func isAbsolutePath(arg string) bool {
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "\\") {
		return true
	}
	if len(arg) >= 2 && arg[1] == ':' && ((arg[0] >= 'a' && arg[0] <= 'z') || (arg[0] >= 'A' && arg[0] <= 'Z')) {
		return true
	}
	return false
}

func isInteractiveShellCommand(token string) bool {
	name := strings.ToLower(token)
	name = strings.TrimSuffix(name, ".exe")
	return interactiveShellNames[name]
}

// runCapturedCommand runs command through the platform shell with cwd = the
// active tab's path, waiting up to commandGraceTimeout. If it finishes in
// time: refresh the tab (files may have changed) and, only if it produced
// output, show it and wait for the user to dismiss it — the Midnight-
// Commander-style "Always pause, but only when there's something to see"
// behavior Allan asked for (so a redirected `dir > file.txt`, which prints
// nothing, just completes quietly). If it's still running when the grace
// period elapses, stop waiting synchronously and let it keep running in the
// background without opening the output pane — the general fallback for
// anything not in interactiveShellNames that turns out to be long-running
// (a GUI app, an editor, ...), rather than blocking the UI on it.
func (c *commander) runCapturedCommand(command string, v *fileListView) {
	shellBin, shellArgs := shellInvocation(command)
	cmd := exec.Command(shellBin, shellArgs...)
	cmd.Dir = v.state.Path
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Start(); err != nil {
		c.showStatus("command failed to start: " + err.Error())
		return
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	go func() {
		select {
		case <-done:
			fyne.Do(func() {
				v.Reload()
				out := strings.TrimSpace(buf.String())
				if out != "" {
					c.showCmdOutput(out)
				} else {
					c.hideCmdOutput()
				}
			})
		case <-time.After(commandGraceTimeout):
			// Still running: leave it be. The done-goroutine above still
			// completes independently whenever the process actually exits;
			// nothing further is shown for it.
		}
	}()
}

func shellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}

// launchInteractiveShell opens a real, independent terminal window rooted
// at cwd — used when the user types a shell's own name (cmd/powershell/
// bash/...), which needs an actual tty to be usable, not captured output.
// shellCmd is the exact token the user typed (e.g. "powershell" vs "cmd"):
// honored on Windows, where it picks which real shell the new console runs;
// macOS/Linux terminal emulators just launch the user's default login shell
// regardless (Terminal.app/xterm don't offer an equally simple per-shell
// override), which is an acceptable simplification for how rarely those
// differ from what was typed.
func launchInteractiveShell(shellCmd, cwd string) error {
	switch runtime.GOOS {
	case "windows":
		return launchInteractiveShellWindows(shellCmd, cwd)
	case "darwin":
		// `open -a Terminal <dir>` opens a new Terminal.app window already
		// cd'd to dir — the same open-launches-Terminal quirk internal/
		// launch's Open doc comment already documents for a bare executable.
		return launch.Run("open", launch.RunOptions{Args: []string{"-a", "Terminal", cwd}})
	default:
		return launchInteractiveShellLinux(cwd)
	}
}

// linuxTerminalCandidate is one terminal emulator to try, in order, on
// Linux — best-effort, matching this app's existing "least-used platform,
// try common tools, fail gracefully" convention (see the paused Linux XDND
// drag-out in ReleaseNotes.txt).
type linuxTerminalCandidate struct {
	binary string
	args   func(cwd string) []string
}

var linuxTerminalCandidates = []linuxTerminalCandidate{
	{"x-terminal-emulator", func(cwd string) []string { return []string{"--working-directory=" + cwd} }},
	{"gnome-terminal", func(cwd string) []string { return []string{"--working-directory=" + cwd} }},
	{"konsole", func(cwd string) []string { return []string{"--workdir", cwd} }},
	{"xterm", func(cwd string) []string {
		return []string{"-e", "sh", "-c", fmt.Sprintf("cd %q && exec $SHELL", cwd)}
	}},
}

func launchInteractiveShellLinux(cwd string) error {
	for _, cand := range linuxTerminalCandidates {
		if _, err := exec.LookPath(cand.binary); err == nil {
			return launch.Run(cand.binary, launch.RunOptions{Args: cand.args(cwd)})
		}
	}
	return errors.New("no terminal emulator found on PATH")
}
