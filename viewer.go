// viewer.go — F3's read-only viewer: plain text, or a classic hex dump for
// anything that sniffs as binary (a null byte in the first few KB).
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// viewerMaxBytes caps how much of a file this simple in-memory viewer reads.
const viewerMaxBytes = 4 << 20 // 4 MB

func (c *commander) doView() {
	paths := c.activePane().activeView().SelectionOrCursor()
	if len(paths) == 0 {
		c.showStatus("select a file to view")
		return
	}
	path := paths[0]
	info, err := os.Stat(path)
	if err != nil {
		c.showStatus("cannot view " + path + ": " + err.Error())
		return
	}
	if info.IsDir() {
		c.showStatus("F3: select a file, not a directory")
		return
	}
	showViewer(c.app, path)
}

func showViewer(a fyne.App, path string) {
	win := a.NewWindow("View: " + filepath.Base(path))
	win.SetIcon(resourceKrankyBearCommanderPng)

	data, truncated, err := readCapped(path, viewerMaxBytes)
	var body string
	switch {
	case err != nil:
		body = "Error reading file: " + err.Error()
	case looksBinary(data):
		body = hexDump(data)
	default:
		body = string(data)
	}
	if truncated {
		body += fmt.Sprintf("\n\n… (truncated; showing first %s)", humanSize(viewerMaxBytes))
	}

	label := widget.NewLabel(body)
	label.Wrapping = fyne.TextWrapOff
	label.TextStyle = fyne.TextStyle{Monospace: true}

	win.SetContent(container.NewScroll(label))
	win.Resize(fyne.NewSize(800, 600))
	win.SetCloseIntercept(win.Hide)
	win.Show()
}

// readCapped reads up to max bytes of path, reporting whether the file was
// larger than that.
func readCapped(path string, max int64) (data []byte, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	buf := make([]byte, max+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false, err
	}
	if int64(n) > max {
		return buf[:max], true, nil
	}
	return buf[:n], false, nil
}

// looksBinary sniffs the first few KB for a null byte, the same heuristic
// `file`/git use to call something binary.
func looksBinary(data []byte) bool {
	sniff := data
	if len(sniff) > 8000 {
		sniff = sniff[:8000]
	}
	return bytes.IndexByte(sniff, 0) >= 0
}

// hexDump renders data as a classic 16-bytes-per-line offset/hex/ASCII dump.
func hexDump(data []byte) string {
	var b strings.Builder
	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]

		fmt.Fprintf(&b, "%08x  ", i)
		for j := 0; j < 16; j++ {
			if j < len(chunk) {
				fmt.Fprintf(&b, "%02x ", chunk[j])
			} else {
				b.WriteString("   ")
			}
			if j == 7 {
				b.WriteByte(' ')
			}
		}
		b.WriteString(" |")
		for _, ch := range chunk {
			if ch >= 32 && ch < 127 {
				b.WriteByte(ch)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString("|\n")
	}
	return b.String()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
