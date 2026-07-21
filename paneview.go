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
	fs           vfs.FileSystem
	win          fyne.Window
	colors       func() ColorScheme
	isActivePane func() bool
	onActivated  func() // this pane was clicked into; tell commander to make it active
	onStatus     func(msg string)
	onOtherKey   func(*fyne.KeyEvent) // forwarded to each tab's fileListView — see keyTable

	tabs   *container.DocTabs
	views  []*fileListView
	states []*panelstate.State

	statusLabel *widget.Label
	lockBtn     *ttwidget.Button

	root fyne.CanvasObject
}

func newPane(fs vfs.FileSystem, win fyne.Window, colors func() ColorScheme, isActivePane func() bool, onActivated func(), onStatus func(string), onOtherKey func(*fyne.KeyEvent)) *pane {
	p := &pane{fs: fs, win: win, colors: colors, isActivePane: isActivePane, onActivated: onActivated, onStatus: onStatus, onOtherKey: onOtherKey}

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

	toolbar := container.NewHBox(p.lockBtn, homeBtn, briefBtn, fullBtn)

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
	view := newFileListView(p.fs, state, p.colors, p.isActivePane)
	view.onNavigated = func() { p.refreshChrome() }
	view.onStatus = p.onStatus
	view.onFocusGained = p.onActivated
	view.onSelection = func(count int, size int64) { p.updateStatusLine(count, size) }
	view.onOtherKey = p.onOtherKey

	item := container.NewTabItem(tabLabel(state), view.Build())
	p.views = append(p.views, view)
	p.states = append(p.states, state)
	return item
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
	p.statusLabel.SetText(state.Path)
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
	state := p.activeState()
	if state == nil {
		return
	}
	if count > 0 {
		p.statusLabel.SetText(fmt.Sprintf("%s   [%d selected, %s]", state.Path, count, humanSize(size)))
	} else {
		p.statusLabel.SetText(state.Path)
	}
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
