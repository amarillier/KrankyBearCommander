// paneview.go — one side (left or right) of the dual-pane window: a lockable
// tab strip (container.NewDocTabs, which gives closable tabs and a "+" new-
// tab button for free) plus a small toolbar (lock, home, brief/full view) and
// a status line showing the active tab's path / selection summary.
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/layout"
	"commander/internal/panelstate"
	"commander/internal/vfs"
	"commander/internal/vfs/zipfs"
)

// pane owns one side's tabs; each tab pairs a panelstate.State with the
// fileListView that renders it.
type pane struct {
	fs                   vfs.FileSystem
	win                  fyne.Window
	colors               func() ColorScheme
	showHidden           func() bool // dotfile visibility — shared app-wide setting, see commander.toggleHiddenFiles
	showDriveBar         func() bool // volume/drive toolbar visibility — shared app-wide setting, see commander.toggleDriveBar
	briefColumns         func() int  // Brief view column count (0 = Auto) — shared app-wide setting, see commander.setBriefColumns
	isActivePane         func() bool
	onActivated          func() // this pane was clicked into; tell commander to make it active
	onStatus             func(msg string)
	onOtherKey           func(*fyne.KeyEvent)                                              // forwarded to each tab's fileListView — see keyTable
	onFavorites          func()                                                            // Favorites button clicked; commander owns the shared list (favorites_ui.go)
	onContextMenu        func(p *pane, view *fileListView, name string, pos fyne.Position) // right-click on a row; commander owns the menu (contextmenu_ui.go)
	onSearch             func()                                                            // Search button clicked; commander owns the search dialog (search_ui.go)
	onConnections        func()                                                            // Connection button clicked; commander owns the Connections manager (connections_ui.go), opening a new tab in THIS pane
	onOpenArchivedMember func(zfs *zipfs.FS, name, presentedPath string)                   // Enter/double-click on a file inside an open archive; commander extracts + opens it (archive_browse_ui.go)
	onEject              func(root string) error                                           // Eject clicked on a drive button; commander navigates both panes off it first (drivebutton_ui.go)
	onRefreshAll         func()                                                            // this pane's own ⟳ drive-bar button clicked; commander refreshes both panes (see commander.doRefresh) rather than just this one

	tabs   *container.DocTabs
	views  []*fileListView
	states []*panelstate.State

	statusLabel *widget.Label
	lockBtn     *ttwidget.Button
	driveBar    *container.Scroll

	lastCursorInfo string
	lastSelCount   int
	lastSelSize    int64

	root fyne.CanvasObject
}

func newPane(fs vfs.FileSystem, win fyne.Window, colors func() ColorScheme, showHidden func() bool, showDriveBar func() bool, briefColumns func() int, isActivePane func() bool, onActivated func(), onStatus func(string), onOtherKey func(*fyne.KeyEvent), onFavorites func(), onContextMenu func(p *pane, view *fileListView, name string, pos fyne.Position), onSearch func(), onConnections func(), onOpenArchivedMember func(zfs *zipfs.FS, name, presentedPath string), onEject func(root string) error, onRefreshAll func()) *pane {
	p := &pane{fs: fs, win: win, colors: colors, showHidden: showHidden, showDriveBar: showDriveBar, briefColumns: briefColumns, isActivePane: isActivePane, onActivated: onActivated, onStatus: onStatus, onOtherKey: onOtherKey, onFavorites: onFavorites, onContextMenu: onContextMenu, onSearch: onSearch, onConnections: onConnections, onOpenArchivedMember: onOpenArchivedMember, onEject: onEject, onRefreshAll: onRefreshAll}

	p.statusLabel = widget.NewLabel("")

	// Buttons take keyboard focus on click and, unless cleared, would
	// swallow the next unmodified keypress (e.g. an F-key) instead of
	// letting it reach commander.dispatchKey — see keymap.go's top doc
	// comment and keyBarButton, which does the same for the F-key row.
	unfocus := func() { p.win.Canvas().Unfocus() }

	p.lockBtn = ttwidget.NewButton("🔓", func() { p.onActivated(); p.toggleLock(); unfocus() })
	p.lockBtn.SetToolTip("Lock this tab to its current directory (with a choice of whether subdirectories can still be opened)")

	homeBtn := ttwidget.NewButtonWithIcon("", theme.HomeIcon(), func() { p.onActivated(); p.activateHome(); unfocus() })
	homeBtn.SetToolTip("Go to the locked directory (if locked) or your home directory")

	briefBtn := ttwidget.NewButton("Brief", func() { p.onActivated(); p.setViewMode(panelstate.ViewBrief); unfocus() })
	briefBtn.SetToolTip("Switch to a compact, name-only view")

	fullBtn := ttwidget.NewButton("Full", func() { p.onActivated(); p.setViewMode(panelstate.ViewExpanded); unfocus() })
	fullBtn.SetToolTip("Switch to the detailed view with sortable Name/Size/Modified columns")

	favBtn := ttwidget.NewButton("★", func() { p.onActivated(); p.onFavorites(); unfocus() })
	favBtn.SetToolTip("Favorites: jump to a volume or bookmarked directory, or add/manage bookmarks")

	selectAllBtn := ttwidget.NewButton("☑", func() {
		p.onActivated()
		p.toggleSelectAll()
		unfocus()
	})
	selectAllBtn.SetToolTip("Select All / Deselect All (Ctrl+A / Ctrl+Shift+A, ⌘ on macOS)")

	searchBtn := ttwidget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		p.onActivated()
		if p.onSearch != nil {
			p.onSearch()
		}
		// No unfocus() here, unlike every other button on this toolbar:
		// showSearch deliberately focuses the Search dialog's text field so
		// typing works immediately (see search_ui.go) — calling Unfocus()
		// right after would immediately undo that.
	})
	searchBtn.SetToolTip("Search this tab's directory recursively by name or pattern")

	connectionsBtn := ttwidget.NewButtonWithIcon("", theme.ComputerIcon(), func() {
		p.onActivated()
		if p.onConnections != nil {
			p.onConnections()
		}
		unfocus()
	})
	connectionsBtn.SetToolTip("Connect to a saved remote connection, opening it in a new tab in this pane")

	toolbar := container.NewHBox(p.lockBtn, homeBtn, briefBtn, fullBtn, favBtn, selectAllBtn, searchBtn, connectionsBtn)
	p.driveBar = container.NewHScroll(p.buildDriveBarContent())

	p.tabs = container.NewDocTabs()
	p.tabs.CreateTab = func() *container.TabItem {
		p.onActivated()
		// DocTabs' own "+" button handler appends the returned item and
		// selects it itself (see newTabItem's doc comment) — its OnSelected
		// then fires p.refreshChrome for us.
		return p.newTabItem(panelstate.New(p.defaultHome()))
	}
	p.tabs.OnSelected = func(*container.TabItem) {
		p.onActivated()
		p.refreshChrome()
	}
	p.tabs.CloseIntercept = func(item *container.TabItem) {
		if len(p.tabs.Items) <= 1 {
			return // always keep at least one tab open
		}
		idx := p.indexOf(item)
		if idx < 0 {
			return
		}
		p.views[idx].closeFS() // release an open archive handle, if this tab was browsing one
		p.views = append(p.views[:idx], p.views[idx+1:]...)
		p.states = append(p.states[:idx], p.states[idx+1:]...)
		p.tabs.RemoveIndex(idx)
		p.refreshChrome()
	}

	// No default tab is added here: for a returning user, commander.go's
	// loadLayout() (right after both panes are constructed) replaces
	// whatever's here with the persisted tabs anyway, so building one now
	// would just be thrown-away work — reading a directory, sorting it, and
	// rendering a whole view (Brief's is the app's very first text
	// measurement of the whole run, before the window even exists, which
	// once made a real difference — see ensureAtLeastOneTab). loadLayout
	// calls ensureAtLeastOneTab right after, so a pane never ends up with
	// zero tabs even on a fresh install / corrupt layout file.

	p.root = container.NewBorder(container.NewVBox(toolbar, p.driveBar), p.statusLabel, nil, nil, p.tabs)
	p.refreshDriveBarVisibility()
	return p
}

func (p *pane) indexOf(item *container.TabItem) int {
	for i, it := range p.tabs.Items {
		if it == item {
			return i
		}
	}
	return -1
}

// defaultHome resolves where an unlocked tab's Home / "\" / "/" go, and where
// a brand new tab starts: the user's home directory, falling back to the
// first filesystem root.
func (p *pane) defaultHome() string {
	if home, err := p.fs.HomeDir(); err == nil && home != "" {
		return home
	}
	if roots, err := p.fs.Roots(); err == nil && len(roots) > 0 {
		return roots[0]
	}
	return "."
}

// newTabItem builds the fileListView + TabItem for state and records it in
// p.views/p.states, but does NOT touch p.tabs.Items — callers are
// responsible for that. This split exists because DocTabs.CreateTab's
// contract is "return an item and DocTabs appends it for you" (see its
// buildCreateTabsButton): appending it again ourselves would double-add the
// tab to p.tabs.Items while p.views/p.states only grew once, desyncing the
// parallel slices (and eventually panicking on tab close with an
// out-of-range slice index).
func (p *pane) newTabItem(state *panelstate.State) *container.TabItem {
	view := newFileListView(p.fs, state, p.colors, p.showHidden, p.briefColumns, p.defaultHome, p.isActivePane)
	p.bindView(view)

	item := container.NewTabItem(tabLabel(state), view.Build())
	p.views = append(p.views, view)
	p.states = append(p.states, state)
	return item
}

// bindView (re)points a view's callbacks and active-pane check at p — used
// both for freshly built views and, after swapPanes moves views to a new
// owning pane, to rebind them there.
func (p *pane) bindView(view *fileListView) {
	view.isActive = p.isActivePane
	view.onNavigated = func() { p.refreshChrome() }
	view.onStatus = p.onStatus
	view.onFocusGained = p.onActivated
	view.onSelection = func(count int, size int64) { p.updateStatusLine(count, size) }
	view.onCursorInfo = func(info string) { p.lastCursorInfo = info; p.renderStatusLine() }
	view.onOtherKey = p.onOtherKey
	view.onContextMenu = func(name string, pos fyne.Position) {
		if p.onContextMenu != nil {
			p.onContextMenu(p, view, name, pos)
		}
	}
	view.onOpenArchivedMember = p.onOpenArchivedMember
}

// rebindViews re-binds every view p currently holds — called after
// swapPanes moves views' ownership between panes.
func (p *pane) rebindViews() {
	for _, v := range p.views {
		p.bindView(v)
	}
}

// addTabFromState creates a tab and appends+selects it directly — for call
// sites other than the CreateTab "+" button (initial construction, layout
// restore, the F9 menu's "New Tab"), which must append themselves.
func (p *pane) addTabFromState(state *panelstate.State) *container.TabItem {
	item := p.newTabItem(state)
	p.tabs.Append(item)
	p.tabs.SelectIndex(len(p.tabs.Items) - 1)
	p.refreshChrome()
	return item
}

// addTabFromStateWithFS is addTabFromState, but rooted at an explicit
// vfs.FileSystem instead of the pane's own p.fs — used when connecting to a
// saved Connection (Connections manager, connections_ui.go), where state's
// starting Path is wherever THAT filesystem's own top is, not anywhere
// under p.fs. A separate function rather than a parameter on newTabItem
// itself: building the view with the right fs from the start avoids an
// initial Reload() against the wrong (local) filesystem that overriding
// view.fs after the fact would otherwise waste.
func (p *pane) addTabFromStateWithFS(state *panelstate.State, fs vfs.FileSystem) *container.TabItem {
	view := newFileListView(fs, state, p.colors, p.showHidden, p.briefColumns, p.defaultHome, p.isActivePane)
	p.bindView(view)
	item := container.NewTabItem(tabLabel(state), view.Build())
	p.views = append(p.views, view)
	p.states = append(p.states, state)
	p.tabs.Append(item)
	p.tabs.SelectIndex(len(p.tabs.Items) - 1)
	p.refreshChrome()
	return item
}

func tabLabel(state *panelstate.State) string {
	name := state.Path
	if state.TabTitle != "" {
		name = state.TabTitle
	} else if base := lastPathComponent(state.Path); base != "" {
		name = base
	}
	if state.Locked {
		return "🔒 " + name
	}
	return name
}

func lastPathComponent(path string) string {
	trimmed := path
	for len(trimmed) > 1 && (trimmed[len(trimmed)-1] == '/' || trimmed[len(trimmed)-1] == '\\') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i] == '/' || trimmed[i] == '\\' {
			return trimmed[i+1:]
		}
	}
	return trimmed
}

func (p *pane) activeIndex() int { return p.tabs.SelectedIndex() }

func (p *pane) activeView() *fileListView {
	idx := p.activeIndex()
	if idx < 0 || idx >= len(p.views) {
		return nil
	}
	return p.views[idx]
}

func (p *pane) activeState() *panelstate.State {
	idx := p.activeIndex()
	if idx < 0 || idx >= len(p.states) {
		return nil
	}
	return p.states[idx]
}

func (p *pane) activateHome() {
	if v := p.activeView(); v != nil {
		v.Home(p.defaultHome())
	}
}

// buildDriveBarContent builds the volume/drive toolbar's row: \ (home) and
// .. (up) — duplicating the main toolbar's ⌂ and the file list's own ".."
// row, but grouped here for a dedicated navigation toolbar's clarity —
// then Refresh, then one button per filesystem root (drive letters on
// Windows, "/" elsewhere, plus any mounted external volume — see
// localfs.Roots). Wrapped in an HScroll by the caller so a Windows machine
// with many drive letters doesn't force the pane wider; the fixed nav
// buttons come first so they're always visible without scrolling.
//
// Rebuilt (not just re-laid-out) by rescanDriveBar whenever Refresh is
// clicked: unlike the current directory's contents, there's no portable
// "tell me when a drive was plugged in" signal here, so re-scanning
// Roots() on an explicit user action (rather than trying to hook OS
// device-change notifications, e.g. Windows' WM_DEVICECHANGE) is the
// deliberately simpler choice — see ReleaseNotes.txt's Later-phase note if
// live auto-detection is ever worth the platform-specific work.
func (p *pane) buildDriveBarContent() fyne.CanvasObject {
	unfocus := func() { p.win.Canvas().Unfocus() }

	homeBtn := ttwidget.NewButton("\\", func() { p.onActivated(); p.activateHome(); unfocus() })
	homeBtn.SetToolTip("Go to the locked directory (if locked) or your home directory")

	upBtn := ttwidget.NewButton("..", func() {
		p.onActivated()
		if v := p.activeView(); v != nil {
			v.NavigateUp()
		}
		unfocus()
	})
	upBtn.SetToolTip("Go up one directory level")

	refreshBtn := ttwidget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		p.onActivated()
		if p.onRefreshAll != nil {
			p.onRefreshAll()
		}
		unfocus()
	})
	refreshBtn.SetToolTip("Refresh both panes (F2) and re-scan for newly connected drives")

	items := []fyne.CanvasObject{homeBtn, upBtn, refreshBtn, widget.NewSeparator()}

	roots, err := p.fs.Roots()
	if err != nil {
		roots = nil
	}
	for _, r := range roots {
		root := r
		driveBtn := newDriveButton(root, func() {
			p.onActivated()
			if v := p.activeView(); v != nil {
				v.JumpTo(root)
			}
			unfocus()
		}, func(pos fyne.Position) {
			p.onActivated()
			p.showDriveContextMenu(root, pos)
		})
		driveBtn.SetToolTip("Jump to " + root + " (right-click for Eject/Format)")
		items = append(items, driveBtn)
	}

	return container.NewHBox(items...)
}

// rescanDriveBar rebuilds the volume/drive toolbar's buttons from a fresh
// Roots() call — the only way a newly connected drive (or one that was
// removed) is picked up without restarting the app.
func (p *pane) rescanDriveBar() {
	p.driveBar.Content = p.buildDriveBarContent()
	p.driveBar.Refresh()
}

// refreshDriveBarVisibility shows or hides the volume/drive toolbar per
// the shared showDriveBar setting — called once at construction and again
// whenever commander.toggleDriveBar flips it.
func (p *pane) refreshDriveBarVisibility() {
	if p.showDriveBar != nil && p.showDriveBar() {
		p.driveBar.Show()
	} else {
		p.driveBar.Hide()
	}
}

// toggleSelectAll is the toolbar's Select All/Deselect All button: since
// there's no persistent tri-state indicator, a selection already in
// progress just gets cleared rather than topped up to "everything" — the
// common cases (nothing selected -> select everything, anything selected ->
// start over) both fall out of "toggle by whether anything is selected".
func (p *pane) toggleSelectAll() {
	v := p.activeView()
	if v == nil {
		return
	}
	if v.HasSelection() {
		v.DeselectAll()
	} else {
		v.SelectAll()
	}
}

func (p *pane) setViewMode(mode panelstate.ViewMode) {
	state := p.activeState()
	if state == nil {
		return
	}
	state.ViewMode = mode
	if v := p.activeView(); v != nil {
		v.Reload()
	}
}

// toggleLock unlocks an already-locked active tab immediately, or — for an
// unlocked tab — prompts whether subdirectory navigation should still be
// allowed once locked (the two independent choices described for locked
// tabs: pinned location, and whether "cd" is permitted at all beneath it).
func (p *pane) toggleLock() {
	state := p.activeState()
	if state == nil {
		return
	}
	if state.Locked {
		state.Unlock()
		p.refreshChrome()
		return
	}

	allowNav := ttwidget.NewCheck("Allow navigating into subdirectories", nil)
	allowNav.SetToolTip("On: you can still open subfolders, but Home/\\// always jump back here.\nOff: this tab is fully pinned — no directory changes at all.")
	allowNav.SetChecked(true)
	content := container.NewVBox(
		widget.NewLabel("Lock this tab to:\n"+state.Path),
		allowNav,
	)
	showDialog(dialog.NewCustomConfirm("Lock Tab", "Lock", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		state.Lock(allowNav.Checked)
		p.refreshChrome()
	}, p.win))
}

// refreshChrome syncs the active tab's title and the lock button/status line
// to the active tab's current state — called after anything that might
// change path/lock/selection.
func (p *pane) refreshChrome() {
	idx := p.activeIndex()
	if idx < 0 || idx >= len(p.states) {
		return
	}
	state := p.states[idx]
	item := p.tabs.Items[idx]
	item.Text = tabLabel(state)
	p.tabs.Refresh()

	if state.Locked {
		p.lockBtn.SetText("🔒")
	} else {
		p.lockBtn.SetText("🔓")
	}
	// Switching tabs changes which cursor/selection applies; reset until the
	// newly active view reports its own (Reload, called when a tab is built
	// or re-selected, does so via onCursorInfo/onSelection).
	p.lastCursorInfo = state.Path
	p.lastSelCount, p.lastSelSize = 0, 0
	p.renderStatusLine()
}

// snapshot captures this pane's tabs/active-tab for persistence.
func (p *pane) snapshot() layout.PaneLayout {
	pl := layout.PaneLayout{ActiveTab: p.activeIndex()}
	for _, s := range p.states {
		pl.Tabs = append(pl.Tabs, layout.FromState(s))
	}
	return pl
}

// restoreFromLayout replaces this pane's tabs (none yet — see newPane) with
// a persisted arrangement.
func (p *pane) restoreFromLayout(pl layout.PaneLayout) {
	for len(p.tabs.Items) > 0 {
		p.tabs.RemoveIndex(0)
	}
	p.views = nil
	p.states = nil

	for _, t := range pl.Tabs {
		p.addTabFromState(t.ToState())
	}
	if pl.ActiveTab >= 0 && pl.ActiveTab < len(p.tabs.Items) {
		p.tabs.SelectIndex(pl.ActiveTab)
	}
	p.refreshChrome()
}

// ensureAtLeastOneTab adds the default Home tab if this pane still has none
// — the case on a fresh install (no saved layout yet) or if the saved
// layout failed to load / had no tabs for this side. commander.go calls
// this right after loadLayout(), once per pane. newPane deliberately
// doesn't add this tab itself: for a returning user it would just be
// immediately discarded by restoreFromLayout, after paying for a real
// directory read, sort, and render (including Brief view's per-cell text
// measurement) before the window has even been shown.
func (p *pane) ensureAtLeastOneTab() {
	if len(p.views) > 0 {
		return
	}
	p.addTabFromState(panelstate.New(p.defaultHome()))
	p.refreshChrome()
}

func (p *pane) updateStatusLine(count int, size int64) {
	p.lastSelCount, p.lastSelSize = count, size
	p.renderStatusLine()
}

// renderStatusLine combines the cursor row's info (name + size/modified, or
// item count for a directory — see fileListView.cursorInfo) with a
// "[N selected, size]" suffix whenever there's an explicit multi-selection.
func (p *pane) renderStatusLine() {
	text := p.lastCursorInfo
	if p.lastSelCount > 0 {
		text = fmt.Sprintf("%s   [%d selected, %s]", text, p.lastSelCount, humanSize(p.lastSelSize))
	}
	p.statusLabel.SetText(text)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
