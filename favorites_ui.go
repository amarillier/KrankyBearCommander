// favorites_ui.go — the Favorites button's popup (Volumes + bookmarked
// directories, shared across both panes) and the "Manage Favorites" dialog.
// internal/favorites owns persistence; this file is the Fyne-facing half.
package main

import (
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
			c.favorites.Add(cand.Label, cand.Path)
		}
	}
}

// ensureHomeFavorite makes sure the home directory is bookmarked, without
// re-adding any of the OTHER default favorites a user may have deliberately
// removed — only Home gets this one-time (idempotent) backfill.
func (c *commander) ensureHomeFavorite() {
	home, err := c.fs.HomeDir()
	if err != nil || home == "" || c.favorites.Has(home) {
		return
	}
	c.favorites.Add("Home", home)
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
func (c *commander) navigatePane(p *pane, path string) {
	if v := p.activeView(); v != nil {
		v.JumpTo(path)
	}
}

// addFavorite bookmarks p's active tab's current directory.
func (c *commander) addFavorite(p *pane) {
	state := p.activeState()
	if state == nil {
		return
	}
	label := lastPathComponent(state.Path)
	if label == "" {
		label = state.Path
	}
	c.favorites.Add(label, state.Path)
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

// showAddFavoriteMenu is the right-click "add to favorites" popup for a
// single directory — see filelist.go's fileListView.onAddFavorite (wired
// from keyTable.TappedSecondary for the Table view, and tappableCell's for
// Brief).
func (c *commander) showAddFavoriteMenu(path, label string, pos fyne.Position) {
	menu := fyne.NewMenu("", fyne.NewMenuItem(`Add "`+label+`" to Favorites`, func() {
		c.favorites.Add(label, path)
		c.saveFavorites()
	}))
	widget.NewPopUpMenu(menu, c.win.Canvas()).ShowAtPosition(pos)
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
		path, label := e.Path, e.Label
		items = append(items, fyne.NewMenuItem(label, func() { c.navigatePane(p, path) }))
	}
	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Add Current Directory…", func() { c.addFavorite(p) }),
		fyne.NewMenuItem("Manage Favorites…", func() { c.showManageFavorites() }),
	)

	menu := fyne.NewMenu("Favorites", items...)
	widget.NewPopUpMenu(menu, c.win.Canvas()).ShowAtPosition(c.favoritesMenuPos(p))
}

// showManageFavorites lists every favorite with a Remove button — the only
// way to drop one, short of re-adding over it (Add de-duplicates by path).
func (c *commander) showManageFavorites() {
	list := container.NewVBox()

	var refresh func()
	refresh = func() {
		var rows []fyne.CanvasObject
		if len(c.favorites.Entries) == 0 {
			rows = append(rows, widget.NewLabel(`No favorites yet — use "Add Current Directory…" from the Favorites menu.`))
		}
		for _, e := range c.favorites.Entries {
			path := e.Path
			removeBtn := widget.NewButton("Remove", func() {
				c.favorites.Remove(path)
				c.saveFavorites()
				refresh()
			})
			row := container.NewBorder(nil, nil, nil, removeBtn, widget.NewLabel(e.Label+"  —  "+e.Path))
			rows = append(rows, row)
		}
		list.Objects = rows
		list.Refresh()
	}
	refresh()

	d := dialog.NewCustom("Manage Favorites", "Close", container.NewVScroll(list), c.win)
	d.Resize(fyne.NewSize(480, 400))
	d.Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
