package osclipboard

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// Linux has no single native clipboard-files API — X11/Wayland clipboards
// just carry arbitrary MIME-typed data, and file managers agree on
// conventions rather than a system call. This shells out to xclip/wl-copy
// (X11/Wayland's standard, narrow, single-purpose clipboard utilities —
// not general automation engines, unlike osascript/PowerShell) using the
// "x-special/gnome-copied-files" convention GNOME/Nautilus (and several
// other file managers) read for Paste, falling back to plain text/uri-list
// on read since that's the most widely honored format across desktops.
// Best-effort: if no clipboard utility is installed, CopyFiles reports a
// clear error and PasteFiles just reports nothing to paste, matching this
// app's existing optional-external-tool pattern (see fsops.SevenZipAvailable).

const gnomeCopiedFilesType = "x-special/gnome-copied-files"

func CopyFiles(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no files to copy")
	}
	bin, baseArgs, err := clipboardWriteCommand()
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	buf.WriteString("copy\n")
	for _, p := range paths {
		buf.WriteString("file://")
		buf.WriteString(p)
		buf.WriteString("\n")
	}

	cmd := exec.Command(bin, baseArgs...)
	cmd.Stdin = &buf
	return cmd.Run()
}

func clipboardWriteCommand() (string, []string, error) {
	if _, err := exec.LookPath("xclip"); err == nil {
		return "xclip", []string{"-selection", "clipboard", "-t", gnomeCopiedFilesType}, nil
	}
	if _, err := exec.LookPath("wl-copy"); err == nil {
		return "wl-copy", []string{"-t", gnomeCopiedFilesType}, nil
	}
	return "", nil, errors.New("no clipboard utility found (install xclip or wl-clipboard)")
}

// PasteFiles reads file paths from the clipboard, trying the GNOME
// convention first and falling back to plain text/uri-list, or nil if
// neither yields anything (including when no clipboard utility is
// installed — treated as "nothing to paste" rather than an error, since
// that's not something the user did wrong).
func PasteFiles() ([]string, error) {
	for _, format := range []string{gnomeCopiedFilesType, "text/uri-list"} {
		bin, args, err := clipboardReadCommand(format)
		if err != nil {
			continue
		}
		out, err := exec.Command(bin, args...).Output()
		if err != nil {
			continue
		}
		if paths := parseURIList(string(out)); len(paths) > 0 {
			return paths, nil
		}
	}
	return nil, nil
}

func clipboardReadCommand(format string) (string, []string, error) {
	if _, err := exec.LookPath("xclip"); err == nil {
		return "xclip", []string{"-selection", "clipboard", "-t", format, "-o"}, nil
	}
	if _, err := exec.LookPath("wl-paste"); err == nil {
		return "wl-paste", []string{"-t", format}, nil
	}
	return "", nil, errors.New("no clipboard utility found")
}

func parseURIList(out string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "", "copy", "cut":
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue // text/uri-list comment line
		}
		if strings.HasPrefix(line, "file://") {
			paths = append(paths, strings.TrimPrefix(line, "file://"))
		}
	}
	return paths
}
