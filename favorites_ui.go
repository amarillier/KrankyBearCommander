// favorites_ui.go — the Favorites button's popup (Volumes + bookmarked
// directories, shared across both panes) and the "Manage Favorites" dialog.
// internal/favorites owns persistence; this file is the Fyne-facing half.
package main

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/favorites"
)

func (c *commander) favoritesPath() string {
	p, err := favorites.DefaultPath(appName)
	if err != nil {
		return ""
	}
	return p
}

// loadFavorites loads the persisted list, seeding platform defaults (see
// favorites.DefaultSeedCandidates) on first run only — i.e. when
// favorites.json doesn't exist yet, not merely when it's empty (so a user
// who deliberately removes every favorite doesn't get them back).
func (c *commander) loadFavorites() {
	path := c.favoritesPath()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		c.seedDefaultFavorites()
		c.saveFavorites()
		return
	}
	if l, err := favorites.Load(path); err == nil {
		c.favorites = l
	}
	c.ensureHomeFavorite() // backfill for favorites.json saved before Home joined the default seed
}

func (c *commander) seedDefaultFavorites() {
	home, err := c.fs.HomeDir()
	if err != nil {
		return
	}
	for _, cand := range favorites.DefaultSeedCandidates(home) {
		if info, err := os.Stat(cand.Path); err == nil && info.IsDir() {
			c.favorites.Add(cand.Label, cand.Path, "")
		}
	}
}

// ensureHomeFavorite makes sure the home directory is bookmarked, without
// re-adding any of the OTHER default favorites a user may have deliberately
// removed — only Home gets this one-time (idempotent) backfill.
func (c *commander) ensureHomeFavorite() {
	home, err := c.fs.HomeDir()
	if err != nil || home == "" || c.favorites.Has(home, "") {
		return
	}
	c.favorites.Add("Home", home, "")
	c.saveFavorites()
}

func (c *commander) saveFavorites() {
	path := c.favoritesPath()
	if path == "" {
		return
	}
	_ = favorites.Save(path, c.favorites)
}

// navigatePane sends p's active tab to path via JumpTo — a Favorites/Volumes
// pick is an explicit destination, not casual in-pane browsing, so it works
// even on a fully locked tab and never redefines that tab's locked root
// (Home afterward still returns to wherever it was locked before the jump).
// Only for a plain local path — Roots/Volumes are always local; a
// connection-backed favorite goes through navigateFavorite instead, since
// JumpTo alone has no way to reconnect a tab that isn't already on the
// right connection (or isn't connected to anything at all).
func (c *commander) navigatePane(p *pane, path string) {
	if v := p.activeView(); v != nil {
		v.JumpTo(path)
	}
}

// navigateFavorite is a Favorites-menu click. A plain local favorite
// (ConnectionID == "") is just navigatePane. One saved against a remote
// connection reconnects first — connectTo opens a fresh tab in p (exactly
// like Connecting from the Connections manager itself), then jumps within
// THAT new tab's own fs to entry.Path once it's actually open, rather than
// trying to JumpTo on p's current (unrelated, or not-yet-connected) tab.
func (c *commander) navigateFavorite(p *pane, entry favorites.Entry) {
	if entry.ConnectionID == "" {
		c.navigatePane(p, entry.Path)
		return
	}
	conn, ok := c.connectionConfig.FindByID(entry.ConnectionID)
	if !ok {
		dialog.ShowError(fmt.Errorf("the saved connection for %q no longer exists", entry.Label), c.win)
		return
	}
	target := entry.Path
	c.connectTo(conn, p, func() {
		if v := p.activeView(); v != nil {
			v.JumpTo(target)
		}
	}, func(err error) {
		dialog.ShowError(fmt.Errorf("connect to %s: %w", conn.Host, err), c.win)
	})
}

// addFavorite bookmarks p's active tab's current directory. Against a
// remote connection, capturing which saved connection this fs came from
// (via hasConnectionID) is what makes the favorite re-navigable later (see
// navigateFavorite) — the connection itself must still exist by then.
func (c *commander) addFavorite(p *pane) {
	state := p.activeState()
	if state == nil {
		return
	}
	view := p.activeView()
	// A listbox view's Path is a synthetic label, not a real, later
	// re-navigable directory — bookmarking it would produce a Favorite that
	// fails (or, worse, silently resolves to nothing useful) the moment
	// it's actually clicked.
	if c.blockIfListbox(view) {
		return
	}
	connectionID := ""
	if idFS, ok := view.fs.(hasConnectionID); ok {
		connectionID = idFS.ConnectionID()
	}
	label := lastPathComponent(state.Path)
	if label == "" {
		label = state.Path
	}
	c.favorites.Add(label, state.Path, connectionID)
	c.saveFavorites()
}

// favoritesMenuPos picks a reasonable popup position without needing the
// exact button geometry: roughly above the toolbar, on p's side of the split.
func (c *commander) favoritesMenuPos(p *pane) fyne.Position {
	size := c.win.Canvas().Size()
	x := size.Width * 0.25
	if p == c.right {
		x = size.Width * 0.75
	}
	return fyne.NewPos(x, 60)
}

func (c *commander) showFavoritesMenu(p *pane) {
	var items []*fyne.MenuItem

	if roots, err := c.fs.Roots(); err == nil {
		for _, r := range roots {
			root := r
			items = append(items, fyne.NewMenuItem(root, func() { c.navigatePane(p, root) }))
		}
	}
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	if len(c.favorites.Entries) == 0 {
		items = append(items, fyne.NewMenuItem("(no favorites yet)", func() {}))
	}
	for _, e := range c.favorites.Entries {
		entry := e
		items = append(items, fyne.NewMenuItem(entry.Label, func() { c.navigateFavorite(p, entry) }))
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Add Current Directory…", func() { c.addFavorite(p) }),
		fyne.NewMenuItem("Manage Favorites…", func() { c.showManageFavorites(p) }),
	)

	menu := fyne.NewMenu("Favorites", items...)
	widget.NewPopUpMenu(menu, c.win.Canvas()).ShowAtPosition(c.favoritesMenuPos(p))
}

// showManageFavorites lists every favorite with a Remove button — clicking
// a row's label selects/highlights it (same convention as showConnections'
// list), and double-clicking it opens it in p, closing this dialog, exactly
// as if you'd picked it from the Favorites menu directly.
func (c *commander) showManageFavorites(p *pane) {
	list := container.NewVBox()
	var d dialog.Dialog
	var selectedKey string // path+"\x00"+connectionID of the highlighted row, "" if none

	var refresh func()
	refresh = func() {
		var rows []fyne.CanvasObject
		if len(c.favorites.Entries) == 0 {
			rows = append(rows, widget.NewLabel(`No favorites yet — use "Add Current Directory…" from the Favorites menu.`))
		}
		for _, e := range c.favorites.Entries {
			entry := e
			key := entry.Path + "\x00" + entry.ConnectionID
			removeBtn := widget.NewButton("Remove", func() {
				c.favorites.Remove(entry.Path, entry.ConnectionID)
				c.saveFavorites()
				refresh()
			})
			labelText := entry.Label + "  —  " + entry.Path
			if entry.ConnectionID != "" {
				if conn, ok := c.connectionConfig.FindByID(entry.ConnectionID); ok {
					labelText = entry.Label + "  —  " + conn.Name + ": " + entry.Path
				} else {
					labelText = entry.Label + "  —  " + entry.Path + "  (saved connection no longer exists)"
				}
			}
			label := newDoubleTappableLabel(labelText,
				func() { selectedKey = key; refresh() },
				func() { c.navigateFavorite(p, entry); d.Hide() },
			)
			if key == selectedKey {
				label.Importance = widget.HighImportance
			}
			row := container.NewBorder(nil, nil, nil, removeBtn, label)
			rows = append(rows, row)
		}
		list.Objects = rows
		list.Refresh()
	}
	refresh()

	d = dialog.NewCustom("Manage Favorites", "Close", container.NewVScroll(list), c.win)
	d.Resize(fyne.NewSize(480, 400))
	showDialog(d)
}

// doubleTappableLabel is a widget.Label reporting both a single tap
// (select/highlight) and a double-tap (open) — used by showManageFavorites.
// Deliberately its own type rather than adding DoubleTapped to connections_
// ui.go's tappableLabel: Fyne's driver delays EVERY single Tapped by the
// double-tap threshold once a widget implements DoubleTappable at all, even
// for an instance with nothing to do on a double-tap — giving Connections
// manager's existing single-tap-to-highlight rows that same lag would be an
// unwanted regression there.
type doubleTappableLabel struct {
	widget.Label
	onTap       func()
	onDoubleTap func()
}

func newDoubleTappableLabel(text string, onTap, onDoubleTap func()) *doubleTappableLabel {
	l := &doubleTappableLabel{onTap: onTap, onDoubleTap: onDoubleTap}
	l.Text = text
	l.ExtendBaseWidget(l)
	return l
}

func (l *doubleTappableLabel) Tapped(*fyne.PointEvent) {
	if l.onTap != nil {
		l.onTap()
	}
}

func (l *doubleTappableLabel) DoubleTapped(*fyne.PointEvent) {
	if l.onDoubleTap != nil {
		l.onDoubleTap()
	}
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
