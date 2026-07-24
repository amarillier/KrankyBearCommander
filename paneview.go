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
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/layout"
	"commander/internal/panelstate"
	"commander/internal/vfs"
)

// pane owns one side's tabs; each tab pairs a panelstate.State with the
// fileListView that renders it.
type pane struct {
	fs            vfs.FileSystem
	win           fyne.Window
	colors        func() ColorScheme
	showHidden    func() bool // dotfile visibility — shared app-wide setting, see commander.toggleHiddenFiles
	isActivePane  func() bool
	onActivated   func() // this pane was clicked into; tell commander to make it active
	onStatus      func(msg string)
	onOtherKey    func(*fyne.KeyEvent)                                              // forwarded to each tab's fileListView — see keyTable
	onFavorites   func()                                                            // Favorites button clicked; commander owns the shared list (favorites_ui.go)
	onContextMenu func(p *pane, view *fileListView, name string, pos fyne.Position) // right-click on a row; commander owns the menu (contextmenu_ui.go)
	onSearch      func()                                                            // Search button clicked; commander owns the search dialog (search_ui.go)

	tabs   *container.DocTabs
	views  []*fileListView
	states []*panelstate.State

	statusLabel *widget.Label
	lockBtn     *ttwidget.Button

	lastCursorInfo string
	lastSelCount   int
	lastSelSize    int64

	root fyne.CanvasObject
}

func newPane(fs vfs.FileSystem, win fyne.Window, colors func() ColorScheme, showHidden func() bool, isActivePane func() bool, onActivated func(), onStatus func(string), onOtherKey func(*fyne.KeyEvent), onFavorites func(), onContextMenu func(p *pane, view *fileListView, name string, pos fyne.Position), onSearch func()) *pane {
	p := &pane{fs: fs, win: win, colors: colors, showHidden: showHidden, isActivePane: isActivePane, onActivated: onActivated, onStatus: onStatus, onOtherKey: onOtherKey, onFavorites: onFavorites, onContextMenu: onContextMenu, onSearch: onSearch}

	p.statusLabel = widget.NewLabel("")

	// Buttons take keyboard focus on click and, unless cleared, would
	// swallow the next unmodified keypress (e.g. an F-key) instead of
	// letting it reach commander.dispatchKey — see keymap.go's top doc
	// comment and keyBarButton, which does the same for the F-key row.
	unfocus := func() { p.win.Canvas().Unfocus() }

	p.lockBtn = ttwidget.NewButton("🔓", func() { p.onActivated(); p.toggleLock(); unfocus() })
	p.lockBtn.SetToolTip("Lock this tab to its current directory (with a choice of whether subdirectories can still be opened)")

	homeBtn := ttwidget.NewButton("⌂", func() { p.onActivated(); p.activateHome(); unfocus() })
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

	searchBtn := ttwidget.NewButton("🔍", func() {
		p.onActivated()
		if p.onSearch != nil {
			p.onSearch()
		}
		unfocus()
	})
	searchBtn.SetToolTip("Search this tab's directory recursively by name or pattern")

	toolbar := container.NewHBox(p.lockBtn, homeBtn, briefBtn, fullBtn, favBtn, selectAllBtn, searchBtn)

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
		p.views = append(p.views[:idx], p.views[idx+1:]...)
		p.states = append(p.states[:idx], p.states[idx+1:]...)
		p.tabs.RemoveIndex(idx)
		p.refreshChrome()
	}

	p.addTabFromState(panelstate.New(p.defaultHome()))

	p.root = container.NewBorder(toolbar, p.statusLabel, nil, nil, p.tabs)
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
	view := newFileListView(p.fs, state, p.colors, p.showHidden, p.isActivePane)
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

func tabLabel(state *panelstate.State) string {
	name := state.Path
	if base := lastPathComponent(state.Path); base != "" {
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
	dialog.NewCustomConfirm("Lock Tab", "Lock", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		state.Lock(allowNav.Checked)
		p.refreshChrome()
	}, p.win).Show()
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

// restoreFromLayout replaces this pane's tabs (the single default tab
// created at construction) with a persisted arrangement.
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
