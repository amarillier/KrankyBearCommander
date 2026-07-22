// filelist.go — one tab's directory listing: Expanded view (widget.Table with
// a manual sortable header) and Brief view (a name-only wrapped grid), both
// painted with the 4-color scheme from colors.go rather than the ambient Fyne
// theme, so panel/normal/selected/cursor colors are exactly what the user
// configured (classic-Norton by default).
package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"commander/internal/launch"
	"commander/internal/panelstate"
	"commander/internal/vfs"
)

// parentEntryName is the synthetic ".." row offered whenever the tab isn't
// already at its filesystem root.
const parentEntryName = ".."

// doubleTapWindow is how quickly a second tap on the same table row must
// follow the first to count as a double-click/open (widget.Table doesn't
// distinguish single/double taps itself, unlike our custom brief-view cells).
const doubleTapWindow = 450 * time.Millisecond

// fileListView renders and drives one tab's directory listing.
type fileListView struct {
	fs       vfs.FileSystem
	state    *panelstate.State
	colors   func() ColorScheme
	isActive func() bool // whether this view's pane is the app's currently-active pane

	onNavigated   func()                                      // Path changed; let paneview refresh its tab title
	onStatus      func(msg string)                            // brief status-line message, e.g. "tab is locked"
	onSelection   func(count int, size int64)                 // selection summary for the pane's status line
	onCursorInfo  func(info string)                           // cursor row's name/size/modified (or item count for a dir)
	onFocusGained func()                                      // a row in this view was clicked; tell paneview to activate this pane
	onOtherKey    func(*fyne.KeyEvent)                        // a key the table itself doesn't handle, while it has focus — see keyTable
	onAddFavorite func(path, label string, pos fyne.Position) // right-click on a directory row; commander owns the shared list

	root       *fyne.Container // Build()'s return value; holds whichever view is active
	table      *keyTable
	header     [4]*widget.Button // Name / Ext / Size / Modified sort buttons
	permHeader *widget.Label     // Permissions column has no sort field, so it's a plain label
	headerRow  fyne.CanvasObject // built once; also sizes Brief view's header-height spacer, so switching views doesn't shift the grid

	entries   []vfs.Entry // current directory's entries, sorted, excluding ".."
	hasParent bool

	// computedSizes/computedParentSize hold "Calculate Folder Sizes" results
	// (see foldersize_ui.go): recursive du -s-style totals for directories,
	// which otherwise just show "<DIR>", and for the current directory
	// itself (shown on the ".." row). Cleared on every Reload since they'd
	// otherwise describe a listing that's no longer current.
	computedSizes      map[string]int64
	computedParentSize *int64

	lastTapRow  int
	lastTapTime time.Time
}

func newFileListView(fs vfs.FileSystem, state *panelstate.State, colors func() ColorScheme, isActive func() bool) *fileListView {
	return &fileListView{fs: fs, state: state, colors: colors, isActive: isActive, lastTapRow: -1}
}

// Build constructs the view's canvas objects and loads the initial listing.
func (v *fileListView) Build() fyne.CanvasObject {
	v.table = v.buildTable()
	v.header[0] = widget.NewButton("", func() { v.setSort(panelstate.SortName) })
	v.header[1] = widget.NewButton("", func() { v.setSort(panelstate.SortExt) })
	v.header[2] = widget.NewButton("", func() { v.setSort(panelstate.SortSize) })
	v.header[3] = widget.NewButton("", func() { v.setSort(panelstate.SortModified) })
	v.permHeader = widget.NewLabel("Perm")
	v.headerRow = container.New(columnsLayout{}, v.header[0], v.header[1], v.header[2], v.header[3], v.permHeader)
	v.root = container.NewStack()
	v.Reload()
	return v.root
}

// Reload re-reads the directory from disk, re-sorts, and re-renders whichever
// view mode is active.
func (v *fileListView) Reload() {
	v.computedSizes = nil
	v.computedParentSize = nil
	entries, err := v.fs.ReadDir(v.state.Path)
	if err != nil {
		if v.onStatus != nil {
			v.onStatus("cannot read " + v.state.Path + ": " + err.Error())
		}
		entries = nil
	}
	v.entries = panelstate.SortEntries(entries, v.state.SortField, v.state.SortAscending)
	v.hasParent = v.fs.Dir(v.state.Path) != v.state.Path
	v.refreshHeaderLabels()
	v.renderActiveView()
	v.reportSelection()
}

// Refresh repaints without re-reading the directory (selection/cursor moved).
func (v *fileListView) Refresh() {
	v.renderActiveView()
}

func (v *fileListView) renderActiveView() {
	switch v.state.ViewMode {
	case panelstate.ViewBrief:
		// An invisible spacer the same height as Full view's header row, so
		// switching between Brief and Full doesn't shift the grid's top edge
		// (and the two panes don't look lopsided when one's Brief and the
		// other's Full).
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(0, v.headerRow.MinSize().Height))
		v.root.Objects = []fyne.CanvasObject{container.NewBorder(spacer, nil, nil, nil, v.buildBriefGrid())}
	default:
		v.table.Refresh()
		v.root.Objects = []fyne.CanvasObject{container.NewBorder(v.headerRow, nil, nil, nil, v.table)}
	}
	v.root.Refresh()
}

func (v *fileListView) refreshHeaderLabels() {
	arrow := func(f panelstate.SortField) string {
		if v.state.SortField != f {
			return ""
		}
		if v.state.SortAscending {
			return " ▲"
		}
		return " ▼"
	}
	v.header[0].SetText("Name" + arrow(panelstate.SortName))
	v.header[1].SetText("Ext" + arrow(panelstate.SortExt))
	v.header[2].SetText("Size" + arrow(panelstate.SortSize))
	v.header[3].SetText("Modified" + arrow(panelstate.SortModified))
}

func (v *fileListView) setSort(field panelstate.SortField) {
	v.state.ToggleSort(field)
	v.Reload()
}

// ── row/name bookkeeping shared by both view modes ──────────────────────────

func (v *fileListView) rowCount() int {
	n := len(v.entries)
	if v.hasParent {
		n++
	}
	return n
}

func (v *fileListView) entryAt(row int) (vfs.Entry, bool) {
	if v.hasParent {
		if row == 0 {
			return vfs.Entry{Name: parentEntryName, IsDir: true}, true
		}
		row--
	}
	if row < 0 || row >= len(v.entries) {
		return vfs.Entry{}, false
	}
	return v.entries[row], true
}

func (v *fileListView) orderedNames() []string {
	names := make([]string, 0, len(v.entries)+1)
	if v.hasParent {
		names = append(names, parentEntryName)
	}
	for _, e := range v.entries {
		names = append(names, e.Name)
	}
	return names
}

// rowColor returns the text color a row/cell should use given cursor/selection
// state. Only the active pane shows its cursor row in TextCursor — the
// inactive pane's cursor is drawn as normal text, so exactly one pane's
// cursor stands out at a time (classic dual-pane behavior) without needing a
// 5th "dimmed cursor" color.
func (v *fileListView) rowColor(cs ColorScheme, name string) color.Color {
	if v.isActive() && v.state.Cursor == name {
		return cs.TextCursor
	}
	if v.state.Selected[name] {
		return cs.TextSelected
	}
	return cs.TextNormal
}

// ── Expanded view (widget.Table) ────────────────────────────────────────────

// keyTable extends widget.Table so a keypress it doesn't itself handle
// (anything but arrows/space) still reaches commander-level F-key/Enter
// dispatch. Fyne's glfw driver only calls the window canvas's SetOnTypedKey
// fallback when NOTHING is focused (internal/driver/glfw/window.go's
// processKeyPressed: `if focused != nil { focused.TypedKey(...) } else {
// onTypedKey(...) }`) — and clicking a row calls Table.Tapped, which grabs
// real keyboard focus for the Table itself. Once focused, Table's own
// TypedKey silently swallows any key it doesn't recognize, so without this
// override, F-keys would stop working the moment a file/directory is
// clicked (they'd only work again once focus moved elsewhere). See
// keymap.go's doc comment for the deeper reason canvas.AddShortcut can't be
// used here either.
//
// The override works by NOT using widget.NewTable (which calls
// t.ExtendBaseWidget(t) on the embedded Table itself, binding Table's
// internal "super" reference — used by Tapped() to decide what to focus —
// to itself). Extending here instead, before that ever happens, makes
// Table's internal Tapped() focus THIS wrapper, so canvas.Focused() reports
// *keyTable and its TypedKey below gets first look at every keypress.
type keyTable struct {
	widget.Table
	onOtherKey     func(*fyne.KeyEvent)
	onSecondaryTap func(*fyne.PointEvent)
}

func newKeyTable(length func() (int, int), create func() fyne.CanvasObject, update func(widget.TableCellID, fyne.CanvasObject), onOtherKey func(*fyne.KeyEvent), onSecondaryTap func(*fyne.PointEvent)) *keyTable {
	t := &keyTable{onOtherKey: onOtherKey, onSecondaryTap: onSecondaryTap}
	t.Length = length
	t.CreateCell = create
	t.UpdateCell = update
	t.ExtendBaseWidget(t)
	return t
}

func (t *keyTable) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyUp, fyne.KeyDown, fyne.KeyLeft, fyne.KeyRight, fyne.KeySpace:
		t.Table.TypedKey(ev) // built-in cursor-move/select handling
	default:
		if t.onOtherKey != nil {
			t.onOtherKey(ev)
		}
	}
}

// TappedSecondary handles right-click. Table has no exported per-cell hit
// test (columnAt/rowAt are private to package widget), so unlike the Brief
// view's tappableCell — which right-clicks the exact cell under the
// pointer — this acts on whatever the CURRENT cursor row is (i.e. left-click
// a row first, then right-click anywhere on the table for its context menu).
func (t *keyTable) TappedSecondary(e *fyne.PointEvent) {
	if t.onSecondaryTap != nil {
		t.onSecondaryTap(e)
	}
}

// Table column indices (Expanded view): Name, Ext, Size, Modified, Perm.
const (
	colName = iota
	colExt
	colSize
	colModified
	colPerm
	tableColumnCount
)

// columnWidths are shared between keyTable's SetColumnWidth calls and the
// header row's columnsLayout below, so the header labels line up exactly
// with the table's actual (unequal) column widths — a plain
// container.NewGridWithColumns divides its row into equal fifths, which
// didn't match.
var columnWidths = [tableColumnCount]float32{
	colName:     260,
	colExt:      60,
	colSize:     90,
	colModified: 150,
	colPerm:     100,
}

// columnsLayout lays out its children left-to-right at exactly columnWidths,
// with theme.Padding() between them — matching how widget.Table spaces its
// own columns (see Table's columnAt: `visibleColWidths[i-1] + padding`).
type columnsLayout struct{}

func (columnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	pad := theme.Padding()
	var width, height float32
	for i, o := range objects {
		if i > 0 {
			width += pad
		}
		width += columnWidths[i]
		if h := o.MinSize().Height; h > height {
			height = h
		}
	}
	return fyne.NewSize(width, height)
}

func (columnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	var x float32
	for i, o := range objects {
		w := columnWidths[i]
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + pad
	}
}

func (v *fileListView) buildTable() *keyTable {
	t := newKeyTable(
		func() (int, int) { return v.rowCount(), tableColumnCount },
		func() fyne.CanvasObject {
			return container.NewStack(
				canvas.NewRectangle(color.Transparent),
				container.NewHBox(widget.NewCheck("", nil), canvas.NewText("", color.White)),
			)
		},
		v.updateCell,
		func(ev *fyne.KeyEvent) {
			if v.onOtherKey != nil {
				v.onOtherKey(ev)
			}
		},
		func(e *fyne.PointEvent) { v.offerAddCursorToFavorites(e.AbsolutePosition) },
	)
	t.SetColumnWidth(colName, columnWidths[colName])
	t.SetColumnWidth(colExt, columnWidths[colExt])
	t.SetColumnWidth(colSize, columnWidths[colSize])
	t.SetColumnWidth(colModified, columnWidths[colModified])
	t.SetColumnWidth(colPerm, columnWidths[colPerm])
	t.OnSelected = v.handleTableTap
	return t
}

func (v *fileListView) updateCell(id widget.TableCellID, o fyne.CanvasObject) {
	stack := o.(*fyne.Container)
	bg := stack.Objects[0].(*canvas.Rectangle)
	hbox := stack.Objects[1].(*fyne.Container)
	check := hbox.Objects[0].(*widget.Check)
	txt := hbox.Objects[1].(*canvas.Text)

	cs := v.colors()
	bg.FillColor = cs.PanelBG
	bg.Refresh()

	entry, ok := v.entryAt(id.Row)
	if !ok {
		txt.Text = ""
		check.Hidden = true
		txt.Refresh()
		return
	}

	txt.Color = v.rowColor(cs, entry.Name)
	check.Hidden = id.Col != colName || entry.Name == parentEntryName

	switch id.Col {
	case colName:
		check.Checked = v.state.Selected[entry.Name]
		name := entry.Name
		check.OnChanged = func(bool) {
			if name == parentEntryName {
				return
			}
			v.state.ToggleSelect(name)
			v.reportSelection()
			v.table.Refresh()
			// widget.Check.Tapped grabs real keyboard focus for itself (its
			// own TypedKey is a no-op), bypassing keyTable's focus
			// interception entirely — left focused, it would silently eat
			// the very next F-key press (e.g. F8 right after checking a
			// box) exactly like the button/table-focus issue keymap.go's
			// doc comment describes. Clearing focus here restores the
			// canvas-level dispatch fallback for the next keypress.
			if canvas := fyne.CurrentApp().Driver().CanvasForObject(v.table); canvas != nil {
				canvas.Unfocus()
			}
		}
		txt.Text = entry.Name
	case colExt:
		if entry.Name == parentEntryName || entry.IsDir {
			txt.Text = ""
		} else {
			txt.Text = fileExt(entry.Name)
		}
	case colSize:
		switch {
		case entry.Name == parentEntryName:
			if v.computedParentSize != nil {
				txt.Text = humanSize(*v.computedParentSize)
			} else {
				txt.Text = ""
			}
		case entry.IsDir:
			if sz, ok := v.computedSizes[entry.Name]; ok {
				txt.Text = humanSize(sz)
			} else {
				txt.Text = "<DIR>"
			}
		default:
			txt.Text = humanSize(entry.Size)
		}
	case colModified:
		if entry.Name == parentEntryName {
			txt.Text = ""
		} else {
			txt.Text = entry.ModTime.Format("2006-01-02 15:04")
		}
	case colPerm:
		if entry.Name == parentEntryName {
			txt.Text = ""
		} else {
			txt.Text = entry.Mode.String()
		}
	}
	check.Refresh()
	txt.Refresh()
}

// fileExt returns name's extension without the leading dot, or "" for a
// dotfile with no extension (".bashrc") or a name with none at all.
func fileExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return ""
	}
	return name[i+1:]
}

func (v *fileListView) handleTableTap(id widget.TableCellID) {
	entry, ok := v.entryAt(id.Row)
	if !ok {
		return
	}
	if v.onFocusGained != nil {
		v.onFocusGained()
	}

	now := time.Now()
	isDouble := id.Row == v.lastTapRow && now.Sub(v.lastTapTime) < doubleTapWindow
	v.lastTapRow, v.lastTapTime = id.Row, now

	v.state.Cursor = entry.Name
	v.table.Refresh()
	v.reportSelection()

	// widget.Table's own Select() silently no-ops (and skips firing
	// OnSelected) when the same cell is tapped twice in a row — which would
	// swallow the second click of a double-click before we ever see it.
	// Unselecting immediately after handling this tap forces the next tap,
	// even on the same cell, to be treated as a fresh selection so
	// OnSelected/handleTableTap reliably fires again.
	v.table.UnselectAll()

	if isDouble {
		v.activate(entry)
	}
}

// ActivateCursor opens/navigates into the cursor row, same as a double-click
// or Enter (see commander.go's doActivateCursor).
func (v *fileListView) ActivateCursor() {
	if v.state.Cursor == "" {
		return
	}
	v.activateByName(v.state.Cursor)
}

// ── Brief view (name-only wrapped grid) ─────────────────────────────────────

func (v *fileListView) buildBriefGrid() fyne.CanvasObject {
	cs := v.colors()
	names := v.orderedNames()
	cells := make([]fyne.CanvasObject, len(names))
	for i, name := range names {
		cells[i] = v.buildBriefCell(name, cs)
	}
	grid := container.NewGridWrap(fyne.NewSize(180, 28), cells...)
	return container.NewVScroll(grid)
}

func (v *fileListView) buildBriefCell(name string, cs ColorScheme) fyne.CanvasObject {
	txt := canvas.NewText(name, v.rowColor(cs, name))
	bg := canvas.NewRectangle(cs.PanelBG)
	content := container.NewStack(bg, container.NewPadded(txt))

	return newTappableCell(content, func() {
		if v.onFocusGained != nil {
			v.onFocusGained()
		}
		v.state.Cursor = name
		v.reportSelection()
		v.renderActiveView()
	}, func() {
		if v.onFocusGained != nil {
			v.onFocusGained()
		}
		v.state.Cursor = name
		v.reportSelection()
		v.activateByName(name)
	}, func(e *fyne.PointEvent) {
		v.offerAddNameToFavorites(name, e.AbsolutePosition)
	})
}

// ── navigation / activation ──────────────────────────────────────────────────

func (v *fileListView) activateByName(name string) {
	entry, ok := entryByName(v.entries, name)
	if name == parentEntryName {
		entry, ok = vfs.Entry{Name: parentEntryName, IsDir: true}, true
	}
	if !ok {
		return
	}
	v.activate(entry)
}

// offerAddCursorToFavorites is the Table view's right-click handler: offers
// to bookmark the cursor row if (and only if) it's a directory. See
// keyTable.TappedSecondary's doc comment for why this acts on the cursor row
// rather than whatever's precisely under the pointer.
func (v *fileListView) offerAddCursorToFavorites(pos fyne.Position) {
	v.offerAddNameToFavorites(v.state.Cursor, pos)
}

// offerAddNameToFavorites is the Brief view's per-cell right-click handler
// (see buildBriefCell) — name is exactly whatever's under the pointer there.
func (v *fileListView) offerAddNameToFavorites(name string, pos fyne.Position) {
	if v.onAddFavorite == nil || name == "" || name == parentEntryName {
		return
	}
	entry, ok := entryByName(v.entries, name)
	if !ok || !entry.IsDir {
		return
	}
	v.onAddFavorite(v.fs.Join(v.state.Path, entry.Name), entry.Name, pos)
}

func entryByName(entries []vfs.Entry, name string) (vfs.Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return vfs.Entry{}, false
}

func (v *fileListView) activate(entry vfs.Entry) {
	if entry.Name == parentEntryName {
		v.navigateTo(v.fs.Dir(v.state.Path))
		return
	}
	if entry.IsDir {
		v.navigateTo(v.fs.Join(v.state.Path, entry.Name))
		return
	}
	openWithOS(v.fs.Join(v.state.Path, entry.Name))
}

// navigateTo is casual in-pane browsing (double-click/Enter into a
// subdirectory or ".."), which a locked tab may refuse — see JumpTo for
// explicit-destination navigation that a lock never blocks.
func (v *fileListView) navigateTo(target string) {
	if !v.state.Navigate(target) {
		if v.onStatus != nil {
			v.onStatus("tab is locked")
		}
		return
	}
	v.reloadAfterNavigate()
}

// JumpTo is an explicit "take me here" navigation (Favorites, Volumes, Home)
// that always works, even on a fully locked tab, and never touches the
// tab's lock — Home afterward still returns to the same locked root as
// before the jump. See panelstate.State.Jump.
func (v *fileListView) JumpTo(target string) {
	v.state.Jump(target)
	v.reloadAfterNavigate()
}

func (v *fileListView) reloadAfterNavigate() {
	v.Reload()
	if v.onNavigated != nil {
		v.onNavigated()
	}
}

// Home always jumps to the locked root (if locked) or defaultHome — see
// panelstate.State.HomeTarget and JumpTo.
func (v *fileListView) Home(defaultHome string) {
	v.JumpTo(v.state.HomeTarget(defaultHome))
}

// ── selection ────────────────────────────────────────────────────────────────

// ToggleSelectCursor implements Space/Insert: toggle the cursor row's
// selection and advance the cursor to the next row (classic MC muscle
// memory).
func (v *fileListView) ToggleSelectCursor() {
	if v.state.Cursor == "" || v.state.Cursor == parentEntryName {
		return
	}
	v.state.ToggleSelect(v.state.Cursor)
	names := v.orderedNames()
	for i, n := range names {
		if n == v.state.Cursor && i+1 < len(names) {
			v.state.Cursor = names[i+1]
			break
		}
	}
	v.reportSelection()
	v.Refresh()
}

// reportSelection tells the pane's status line about both the explicit
// multi-selection (count/size) and the cursor row's own info — called
// whenever either changes.
func (v *fileListView) reportSelection() {
	if v.onSelection != nil {
		var count int
		var total int64
		for _, e := range v.entries {
			if v.state.Selected[e.Name] {
				count++
				total += e.Size
			}
		}
		v.onSelection(count, total)
	}
	if v.onCursorInfo != nil {
		v.onCursorInfo(v.cursorInfo())
	}
}

// cursorInfo describes the cursor row for the status line: name + size +
// modified time for a file, name + immediate item count for a directory
// (not recursive — cheap even for huge trees). Falls back to the current
// path when there's no cursor (fresh tab, or after a Reload with an
// empty directory).
func (v *fileListView) cursorInfo() string {
	switch v.state.Cursor {
	case "":
		return v.state.Path
	case parentEntryName:
		return ".. (parent directory)"
	}
	entry, ok := entryByName(v.entries, v.state.Cursor)
	if !ok {
		return v.state.Path
	}
	if entry.IsDir {
		children, err := v.fs.ReadDir(v.fs.Join(v.state.Path, entry.Name))
		if err != nil {
			return entry.Name + "  <DIR>"
		}
		return fmt.Sprintf("%s  <DIR>  %d item(s)", entry.Name, len(children))
	}
	return fmt.Sprintf("%s  %s  %s", entry.Name, humanSize(entry.Size), entry.ModTime.Format("2006-01-02 15:04"))
}

// SelectionOrCursor returns full paths for the explicit multi-selection, or
// (if nothing is explicitly selected) just the cursor row — the rule F-key
// operations use to decide what they act on.
func (v *fileListView) SelectionOrCursor() []string {
	var names []string
	for _, e := range v.entries {
		if v.state.Selected[e.Name] {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 && v.state.Cursor != "" && v.state.Cursor != parentEntryName {
		names = append(names, v.state.Cursor)
	}
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = v.fs.Join(v.state.Path, n)
	}
	return paths
}

// ── Calculate Folder Sizes (see foldersize_ui.go) ───────────────────────────

// DirEntryNames returns the names of directory entries in the current
// listing, excluding "..".
func (v *fileListView) DirEntryNames() []string {
	var names []string
	for _, e := range v.entries {
		if e.IsDir {
			names = append(names, e.Name)
		}
	}
	return names
}

// FullPath returns name's full path within this tab's current directory.
func (v *fileListView) FullPath(name string) string {
	return v.fs.Join(v.state.Path, name)
}

// CurrentPath is this tab's current directory (the ".." row's own size, once
// computed, describes this directory's total).
func (v *fileListView) CurrentPath() string {
	return v.state.Path
}

// SetComputedSize records name's calculated recursive size and repaints.
func (v *fileListView) SetComputedSize(name string, size int64) {
	if v.computedSizes == nil {
		v.computedSizes = map[string]int64{}
	}
	v.computedSizes[name] = size
	v.Refresh()
}

// SetComputedParentSize records the current directory's own calculated
// recursive size (shown on the ".." row) and repaints.
func (v *fileListView) SetComputedParentSize(size int64) {
	v.computedParentSize = &size
	v.Refresh()
}

// ── small helpers ────────────────────────────────────────────────────────────

// openWithOS opens path (Enter/double-click on a non-directory entry):
// executables are spawned directly and detached (see internal/launch's doc
// comment for why — avoids macOS's `open` wrapping it in a Terminal window,
// and keeps it running after this app quits); anything else goes through the
// platform's default file association.
func openWithOS(path string) {
	_ = launch.Open(path)
}

// humanSize formats a byte count like "1.2 KB" / "3.4 MB".
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// tappableCell wraps arbitrary content (a colored name label, in the brief
// grid view) to make it single/double-tappable — Fyne containers aren't
// tappable on their own, and implementing both Tappable and DoubleTappable
// lets Fyne's own click-timing logic distinguish them (no manual timestamp
// tracking needed here, unlike the table view where OnSelected gives no such
// distinction).
type tappableCell struct {
	widget.BaseWidget
	content        fyne.CanvasObject
	onTap          func()
	onDoubleTap    func()
	onSecondaryTap func(*fyne.PointEvent)
}

func newTappableCell(content fyne.CanvasObject, onTap, onDoubleTap func(), onSecondaryTap func(*fyne.PointEvent)) *tappableCell {
	c := &tappableCell{content: content, onTap: onTap, onDoubleTap: onDoubleTap, onSecondaryTap: onSecondaryTap}
	c.ExtendBaseWidget(c)
	return c
}

func (c *tappableCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

func (c *tappableCell) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *tappableCell) DoubleTapped(*fyne.PointEvent) {
	if c.onDoubleTap != nil {
		c.onDoubleTap()
	}
}

func (c *tappableCell) TappedSecondary(e *fyne.PointEvent) {
	if c.onSecondaryTap != nil {
		c.onSecondaryTap(e)
	}
}
