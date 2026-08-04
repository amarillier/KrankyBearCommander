// commander.go — the top-level dual-pane container: builds the left/right
// panes, owns which one is "active" (for cursor-color highlighting and which
// pane F-key operations act on), and wires the F1-F10 shortcuts/function-key
// bar (keymap.go) plus window-layout persistence (internal/layout).
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"commander/internal/connections"
	"commander/internal/editors"
	"commander/internal/favorites"
	"commander/internal/launchers"
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

	colorScheme      ColorScheme
	activePaneIndex  int                // 0 = left, 1 = right
	favorites        favorites.List     // shared across both panes — see favorites_ui.go
	editorConfig     editors.Config     // F4 preference — see editors_ui.go
	connectionConfig connections.Config // saved remote connections — see connections_ui.go
	launcherConfig   launchers.Config   // Application Launcher's saved apps — see launcher_ui.go
	// launcherPopupAdd is non-nil only while the launcher popup is showing
	// (set/cleared by showLauncherMenu) — lets dragdrop_ui.go's
	// handleDropped add a dropped file as a launcher instead of copying it
	// into a pane, without either file needing to know the other's details.
	launcherPopupAdd func(name, command string)
	showHiddenFiles  bool   // dotfile visibility, shared across both panes — see toggleHiddenFiles
	showDriveBar     bool   // volume/drive toolbar visibility, shared across both panes — see toggleDriveBar
	briefColumns     int    // Brief view column count, shared across both panes (0 = Auto) — see setBriefColumns
	sevenZipPath     string // optional 7z/7za/7zz binary override — see archive_ui.go

	left  *pane
	right *pane

	split     *container.Split
	statusBar *widget.Label
	root      fyne.CanvasObject
}

func newCommander(a fyne.App, win fyne.Window) *commander {
	c := &commander{app: a, win: win, fs: localfs.New()}
	c.colorScheme = loadColorScheme(a)
	c.showHiddenFiles = a.Preferences().Bool(prefShowHiddenFiles)
	c.showDriveBar = a.Preferences().BoolWithFallback(prefShowDriveBar, true)
	c.briefColumns = a.Preferences().IntWithFallback(prefBriefColumns, 0)
	loadColumnWidths(a) // before any pane/view is built, so their initial SetColumnWidth calls already reflect this
	c.statusBar = widget.NewLabel("")

	c.loadFavorites()
	c.loadEditors()
	c.loadConnections()
	c.loadLaunchers()
	c.loadSevenZipPath()

	c.left = newPane(c.fs, win, c.colors, func() bool { return c.showHiddenFiles }, func() bool { return c.showDriveBar }, func() int { return c.briefColumns }, func() bool { return c.activePaneIndex == 0 }, func() { c.setActivePane(0) }, c.showStatus, c.dispatchKey, func() { c.showFavoritesMenu(c.left) }, c.showRowContextMenu, func() { c.showSearch(c.left) }, func() { c.showConnections(c.left) }, func() { c.showLauncherMenu(c.left) }, c.openArchivedMember, c.ejectDrive, c.doRefresh)
	c.right = newPane(c.fs, win, c.colors, func() bool { return c.showHiddenFiles }, func() bool { return c.showDriveBar }, func() int { return c.briefColumns }, func() bool { return c.activePaneIndex == 1 }, func() { c.setActivePane(1) }, c.showStatus, c.dispatchKey, func() { c.showFavoritesMenu(c.right) }, c.showRowContextMenu, func() { c.showSearch(c.right) }, func() { c.showConnections(c.right) }, func() { c.showLauncherMenu(c.right) }, c.openArchivedMember, c.ejectDrive, c.doRefresh)

	c.split = container.NewHSplit(c.left.root, c.right.root)
	c.split.Offset = 0.5

	keyBar := c.buildFunctionKeyBar()
	bottom := container.NewVBox(c.statusBar, keyBar)
	c.root = container.NewBorder(nil, bottom, nil, nil, c.split)

	c.registerShortcuts()
	c.loadLayout()
	c.left.ensureAtLeastOneTab()
	c.right.ensureAtLeastOneTab()
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

// toggleActivePane is Ctrl+Tab: switches which pane is active, the same as
// clicking into the other one — see registerShortcuts for why Ctrl+Tab
// rather than plain Tab.
func (c *commander) toggleActivePane() {
	if c.activePaneIndex == 0 {
		c.setActivePane(1)
	} else {
		c.setActivePane(0)
	}
}

// swapPanes exchanges the left and right panes' entire tab contents (paths,
// locks, view modes, sort, selection) — which pane is "active" stays with
// the visual slot (left/right), not the content, matching classic
// dual-pane-commander "swap panels" behavior.
func (c *commander) swapPanes() {
	c.left.views, c.right.views = c.right.views, c.left.views
	c.left.states, c.right.states = c.right.states, c.left.states

	leftItems, rightItems := c.left.tabs.Items, c.right.tabs.Items
	leftActive, rightActive := c.left.tabs.SelectedIndex(), c.right.tabs.SelectedIndex()

	c.left.tabs.SetItems(rightItems)
	c.right.tabs.SetItems(leftItems)

	// Each view's callbacks (onNavigated/onStatus/onFocusGained/onOtherKey/
	// onSelection) and isActive check were bound to whichever pane built it;
	// after moving views across, those must point at their new owner.
	c.left.rebindViews()
	c.right.rebindViews()

	if rightActive >= 0 {
		c.left.tabs.SelectIndex(rightActive)
	}
	if leftActive >= 0 {
		c.right.tabs.SelectIndex(leftActive)
	}

	c.left.refreshChrome()
	c.right.refreshChrome()
}

// prefShowHiddenFiles persists the dotfile-visibility toggle the same way
// colors.go persists the color scheme — a single app-wide setting shared by
// both panes (View menu / F9 popup), not a per-tab one.
const prefShowHiddenFiles = "showHiddenFiles"

// toggleHiddenFiles flips dotfile visibility, persists it, and reloads every
// open tab in both panes so the change is visible immediately, not just in
// whichever tab happens to be active.
func (c *commander) toggleHiddenFiles() {
	c.showHiddenFiles = !c.showHiddenFiles
	c.app.Preferences().SetBool(prefShowHiddenFiles, c.showHiddenFiles)
	for _, v := range c.left.views {
		v.Reload()
	}
	for _, v := range c.right.views {
		v.Reload()
	}
}

// prefShowDriveBar persists the volume/drive toolbar's visibility, same
// pattern as prefShowHiddenFiles — defaults to shown (true) so the new
// toolbar is discoverable on first run rather than needing to be found
// via a menu first.
const prefShowDriveBar = "showDriveBar"

// toggleDriveBar flips the volume/drive toolbar's visibility in both panes
// and persists it.
func (c *commander) toggleDriveBar() {
	c.showDriveBar = !c.showDriveBar
	c.app.Preferences().SetBool(prefShowDriveBar, c.showDriveBar)
	c.left.refreshDriveBarVisibility()
	c.right.refreshDriveBarVisibility()
}

// prefBriefColumns persists Brief view's column count, same pattern as
// prefShowHiddenFiles — 0 means Auto (fills the pane width at a fixed cell
// width, the original behavior).
const prefBriefColumns = "briefColumns"

// briefColumnChoices is every selectable column count, in menu order —
// "reasonable maximum" per user request, since more columns than this
// leaves too little width per name to be useful.
var briefColumnChoices = []int{0, 2, 3, 4}

func briefColumnLabel(n int) string {
	if n == 0 {
		return "Auto"
	}
	return fmt.Sprintf("%d Columns", n)
}

// setBriefColumns changes Brief view's column count in both panes and
// persists it — same pattern as toggleHiddenFiles.
func (c *commander) setBriefColumns(n int) {
	c.briefColumns = n
	c.app.Preferences().SetInt(prefBriefColumns, n)
	for _, v := range c.left.views {
		v.Reload()
	}
	for _, v := range c.right.views {
		v.Reload()
	}
}

// buildBriefColumnsSubmenu is Auto/2/3/4, checkmarking the current choice —
// used by both the View menu and the F9 popup (buildEditorsSubmenu in
// editors_ui.go is the same pattern). afterSelect runs after the setting
// changes — the main menu bar (unlike the F9 popup, rebuilt fresh on every
// open) is persistent, so main.go passes a callback that rebuilds it via
// fyne.Do so the new checkmark actually shows; the F9 popup passes nil.
func (c *commander) buildBriefColumnsSubmenu(afterSelect func()) *fyne.Menu {
	items := make([]*fyne.MenuItem, len(briefColumnChoices))
	for i, n := range briefColumnChoices {
		n := n
		item := fyne.NewMenuItem(briefColumnLabel(n), func() {
			c.setBriefColumns(n)
			if afterSelect != nil {
				afterSelect()
			}
		})
		item.Checked = c.briefColumns == n
		items[i] = item
	}
	return fyne.NewMenu("", items...)
}

// columnResized is every columnResizeHandle's entry point (via the global
// cmdr, the same way main.go's menu/tray callbacks reach it): column
// widths are shared/app-wide, so a resize in any one tab's Full view must
// immediately reach every other open tab in both panes, not just the one
// being dragged. Persisting only once the drag actually finishes (final)
// avoids a Preferences write on every pixel of movement.
func (c *commander) columnResized(col int, width float32, final bool) {
	for _, v := range c.left.views {
		v.applyColumnWidth(col, width)
	}
	for _, v := range c.right.views {
		v.applyColumnWidth(col, width)
	}
	if final {
		c.app.Preferences().SetFloat(prefColumnWidthKeys[col], float64(width))
	}
}

func (c *commander) selectAllActive() {
	if v := c.activePane().activeView(); v != nil {
		v.SelectAll()
	}
}

func (c *commander) deselectAllActive() {
	if v := c.activePane().activeView(); v != nil {
		v.DeselectAll()
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
