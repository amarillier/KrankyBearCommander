// commander.go — the top-level dual-pane container: builds the left/right
// panes, owns which one is "active" (for cursor-color highlighting and which
// pane F-key operations act on), and wires the F1-F10 shortcuts/function-key
// bar (keymap.go) plus window-layout persistence (internal/layout).
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"commander/internal/layout"
	"commander/internal/vfs"
	"commander/internal/vfs/localfs"
)

// cmdr is the single running commander instance — module-level like
// help.go's helpWindow / about.go's aboutWindow, since this app has exactly
// one main window's worth of dual-pane state, and menu/tray callbacks
// (main.go) and the quit path need to reach it.
var cmdr *commander

// commander owns the dual-pane layout: which pane is active, the color
// scheme (colors.go), and the function-key row (keymap.go).
type commander struct {
	app fyne.App
	win fyne.Window
	fs  vfs.FileSystem

	colorScheme     ColorScheme
	activePaneIndex int // 0 = left, 1 = right

	left  *pane
	right *pane

	split     *container.Split
	statusBar *widget.Label
	root      fyne.CanvasObject
}

func newCommander(a fyne.App, win fyne.Window) *commander {
	c := &commander{app: a, win: win, fs: localfs.New()}
	c.colorScheme = loadColorScheme(a)
	c.statusBar = widget.NewLabel("")

	c.left = newPane(c.fs, win, c.colors, func() bool { return c.activePaneIndex == 0 }, func() { c.setActivePane(0) }, c.showStatus, c.dispatchKey)
	c.right = newPane(c.fs, win, c.colors, func() bool { return c.activePaneIndex == 1 }, func() { c.setActivePane(1) }, c.showStatus, c.dispatchKey)

	c.split = container.NewHSplit(c.left.root, c.right.root)
	c.split.Offset = 0.5

	keyBar := c.buildFunctionKeyBar()
	bottom := container.NewVBox(c.statusBar, keyBar)
	c.root = container.NewBorder(nil, bottom, nil, nil, c.split)

	c.registerShortcuts()
	c.loadLayout()
	return c
}

// colors is passed to panes/fileListViews as a live-reading accessor so a
// color-scheme change (colors.go's settings dialog) is picked up by the next
// repaint without needing to thread a new value through every tab.
func (c *commander) colors() ColorScheme { return c.colorScheme }

func (c *commander) applyColorScheme(cs ColorScheme) {
	c.colorScheme = cs
	if v := c.left.activeView(); v != nil {
		v.Refresh()
	}
	if v := c.right.activeView(); v != nil {
		v.Refresh()
	}
}

// showStatus is safe to call from any goroutine (file-op progress callbacks
// included) — CLAUDE.md's fyne.Do rule for non-main-goroutine UI updates.
func (c *commander) showStatus(msg string) {
	fyne.Do(func() { c.statusBar.SetText(msg) })
}

func (c *commander) setActivePane(idx int) {
	if c.activePaneIndex == idx {
		return
	}
	c.activePaneIndex = idx
	if v := c.left.activeView(); v != nil {
		v.Refresh()
	}
	if v := c.right.activeView(); v != nil {
		v.Refresh()
	}
}

func (c *commander) togglePane() {
	if c.activePaneIndex == 0 {
		c.setActivePane(1)
	} else {
		c.setActivePane(0)
	}
}

func (c *commander) activePane() *pane {
	if c.activePaneIndex == 1 {
		return c.right
	}
	return c.left
}

func (c *commander) inactivePane() *pane {
	if c.activePaneIndex == 1 {
		return c.left
	}
	return c.right
}

// ── layout persistence ───────────────────────────────────────────────────────

func (c *commander) layoutPath() string {
	p, err := layout.DefaultPath(appName)
	if err != nil {
		return ""
	}
	return p
}

func (c *commander) loadLayout() {
	path := c.layoutPath()
	if path == "" {
		return
	}
	l, err := layout.Load(path)
	if err != nil {
		return
	}
	if len(l.Left.Tabs) > 0 {
		c.left.restoreFromLayout(l.Left)
	}
	if len(l.Right.Tabs) > 0 {
		c.right.restoreFromLayout(l.Right)
	}
	if l.SplitOffset > 0 {
		c.split.Offset = l.SplitOffset
		c.split.Refresh()
	}
}

func (c *commander) saveLayout() {
	path := c.layoutPath()
	if path == "" {
		return
	}
	l := layout.Layout{
		Left:        c.left.snapshot(),
		Right:       c.right.snapshot(),
		SplitOffset: c.split.Offset,
	}
	_ = layout.Save(path, l)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
