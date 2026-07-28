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
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/launch"
	"commander/internal/panelstate"
	"commander/internal/vfs"
	"commander/internal/vfs/localfs"
	"commander/internal/vfs/zipfs"
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
	fs           vfs.FileSystem
	state        *panelstate.State
	colors       func() ColorScheme
	showHidden   func() bool   // dotfile visibility — shared app-wide setting, see commander.toggleHiddenFiles
	briefColumns func() int    // Brief view column count (0 = Auto, fills width at a fixed cell width) — shared app-wide setting, see commander.setBriefColumns
	defaultHome  func() string // pane.defaultHome — where Reload jumps this tab if its current directory has vanished (see Reload)
	isActive     func() bool   // whether this view's pane is the app's currently-active pane

	onNavigated   func()                               // Path changed; let paneview refresh its tab title
	onStatus      func(msg string)                     // brief status-line message, e.g. "tab is locked"
	onSelection   func(count int, size int64)          // selection summary for the pane's status line
	onCursorInfo  func(info string)                    // cursor row's name/size/modified (or item count for a dir)
	onFocusGained func()                               // a row in this view was clicked; tell paneview to activate this pane
	onOtherKey    func(*fyne.KeyEvent)                 // a key the table itself doesn't handle, while it has focus — see keyTable
	onContextMenu func(name string, pos fyne.Position) // right-click on a row; commander owns the popup (contextmenu_ui.go)

	// onOpenArchivedMember is Enter/double-click on a file that's already
	// inside an open archive (v.fs is a *zipfs.FS) — there's no real file at
	// its presented path, so commander (archive_browse_ui.go) extracts it to
	// a temp copy first, then opens that with the OS's default application.
	onOpenArchivedMember func(zfs *zipfs.FS, name, presentedPath string)

	root        *fyne.Container // Build()'s return value; holds whichever view is active
	table       *keyTable
	header      [4]*widget.Button // Name / Ext / Size / Modified sort buttons
	permHeader  *widget.Label     // Permissions column has no sort field, so it's a plain label
	headerRow   fyne.CanvasObject // built once; also sizes Brief view's header-height spacer, so switching views doesn't shift the grid
	briefScroll *container.Scroll // Brief view's own scroll container — rebuilt every render (buildBriefGrid), kept here so ScrollToCursor can reach it later

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

	// selectAnchor is the row name a Shift-click range extends from — it only
	// moves on a plain or Ctrl click, so repeated Shift-clicks keep
	// extending/shrinking the same range instead of re-basing from wherever
	// the previous Shift-click landed (classic Explorer/Finder behavior).
	selectAnchor string

	// renaming/activeRenameField track an in-progress inline rename (see
	// rename_ui.go) — renaming is nil except between beginInlineRename and
	// its eventual commit/cancel; activeRenameField is set by whichever of
	// updateCell/buildBriefCell rendered the matching row, so
	// beginInlineRename can focus and select it right after triggering a
	// Refresh (both view modes' cell objects are only reachable from there).
	renaming          *renamingState
	activeRenameField *renameEntry
}

func newFileListView(fs vfs.FileSystem, state *panelstate.State, colors func() ColorScheme, showHidden func() bool, briefColumns func() int, defaultHome func() string, isActive func() bool) *fileListView {
	return &fileListView{fs: fs, state: state, colors: colors, showHidden: showHidden, briefColumns: briefColumns, defaultHome: defaultHome, isActive: isActive, lastTapRow: -1}
}

// Build constructs the view's canvas objects and loads the initial listing.
func (v *fileListView) Build() fyne.CanvasObject {
	v.table = v.buildTable()
	v.header[0] = widget.NewButton("", func() { v.setSort(panelstate.SortName) })
	v.header[1] = widget.NewButton("", func() { v.setSort(panelstate.SortExt) })
	v.header[2] = widget.NewButton("", func() { v.setSort(panelstate.SortSize) })
	v.header[3] = widget.NewButton("", func() { v.setSort(panelstate.SortModified) })
	v.permHeader = widget.NewLabel("Perm")
	labels := container.New(columnsLayout{}, v.header[0], v.header[1], v.header[2], v.header[3], v.permHeader)
	// One resize handle per boundary BETWEEN columns (not after the last,
	// Perm) — a separate overlay stacked on top of the label row, so
	// adding resize handles doesn't touch columnsLayout's own object list.
	handles := container.New(resizeHandleLayout{},
		newColumnResizeHandle(colName), newColumnResizeHandle(colExt),
		newColumnResizeHandle(colSize), newColumnResizeHandle(colModified))
	v.headerRow = container.NewStack(labels, handles)
	v.root = container.NewStack()
	v.Reload()
	return v.root
}

// Reload re-reads the directory from disk, re-sorts, and re-renders whichever
// view mode is active.
func (v *fileListView) Reload() {
	prevRowCount := v.rowCount()

	// An in-progress inline rename refers to a row in the listing about to
	// be replaced wholesale — nothing currently commits/cancels it first
	// (Reload can run for unrelated reasons, e.g. F2, another tab's
	// operation finishing), so just drop it rather than risk it dangling
	// against a row that may no longer exist.
	v.renaming = nil
	v.activeRenameField = nil

	v.computedSizes = nil
	v.computedParentSize = nil
	entries, err := v.fs.ReadDir(v.state.Path)
	if err != nil && vfs.IsNotExist(err) && v.defaultHome != nil {
		// The tab's directory has genuinely vanished from under it — most
		// commonly an unmounted/disconnected drive (USB, network share) it
		// was sitting in or locked to — rather than leaving the pane stuck
		// showing "no such file or directory" indefinitely, jump home. This
		// is a Jump, not a Navigate: it works even on a fully-locked tab,
		// and never touches Locked/LockedRoot (see panelstate.State.Jump),
		// so a locked tab's Home button still resolves back to the very
		// same (now-missing) locked root afterward — if the drive is
		// reconnected later, Home (or reopening this tab) finds it again.
		// Left alone for any other error (permission denied, a real network
		// timeout, ...) since those are worth the user actually seeing.
		if home := v.defaultHome(); home != "" && home != v.state.Path {
			oldPath := v.state.Path
			v.adjustFSForTarget(home)
			v.state.Jump(home)
			if v.onStatus != nil {
				v.onStatus(oldPath + " is no longer available — moved to home")
			}
			if v.onNavigated != nil {
				v.onNavigated()
			}
			entries, err = v.fs.ReadDir(v.state.Path)
		}
	}
	if err != nil {
		if v.onStatus != nil {
			v.onStatus("cannot read " + v.state.Path + ": " + err.Error())
		}
		entries = nil
	}
	if v.showHidden != nil && !v.showHidden() {
		entries = visibleEntries(entries)
	}
	v.entries = panelstate.SortEntries(entries, v.state.SortField, v.state.SortAscending)
	v.hasParent = v.fs.Dir(v.state.Path) != v.state.Path

	// widget.Table clamps the ROW INDEX it starts drawing from when a
	// scrolled-down listing shrinks underneath it, but not the raw pixel
	// scroll offset used to position that row — so a directory that changes
	// size while a tab sits open and scrolled (e.g. an external process
	// deleting/rewriting files) can leave the Table showing a stale blank
	// gap with only a few rows/dividers rendered near the bottom, all
	// pinned to where the old, longer listing used to be. Snapping back to
	// the top on any row-count change sidesteps it; Brief view doesn't need
	// this since it rebuilds its own scroll container from scratch every
	// render (see renderActiveView).
	if v.table != nil && v.rowCount() != prevRowCount {
		v.table.ScrollToTop()
	}

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

// visibleEntries drops dotfiles/dot-directories (the cross-platform "hidden
// file" convention) when the user hasn't opted into showing them — see
// commander.toggleHiddenFiles.
func visibleEntries(entries []vfs.Entry) []vfs.Entry {
	visible := entries[:0]
	for _, e := range entries {
		if strings.HasPrefix(e.Name, ".") {
			continue
		}
		visible = append(visible, e)
	}
	return visible
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

// rowIndexOf finds name's row index in the current listing.
func (v *fileListView) rowIndexOf(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	for row := 0; row < v.rowCount(); row++ {
		if e, ok := v.entryAt(row); ok && e.Name == name {
			return row, true
		}
	}
	return 0, false
}

// ScrollToCursor brings the cursor row into view in whichever mode is
// currently active — used after Search jumps a tab straight to a match
// (see search_ui.go), since setting Cursor alone positions the logical
// cursor but doesn't itself scroll anything into view.
func (v *fileListView) ScrollToCursor() {
	row, ok := v.rowIndexOf(v.state.Cursor)
	if !ok {
		return
	}
	if v.state.ViewMode == panelstate.ViewBrief {
		v.scrollBriefToRow(row)
		return
	}
	if v.table != nil {
		v.table.ScrollTo(widget.TableCellID{Row: row, Col: colName})
	}
}

// currentBriefColumns is how many columns Brief view's grid is actually
// showing right now: the user's fixed choice (2/3/4 — see
// commander.setBriefColumns), or, in Auto mode, container.NewGridWrap's own
// column count, which it computes internally from the available width and
// doesn't expose — so this replicates that exact formula (see Fyne's
// gridWrapLayout.Layout) from the Brief scroll's current width instead.
func (v *fileListView) currentBriefColumns() int {
	if v.briefColumns != nil {
		if cols := v.briefColumns(); cols > 0 {
			return cols
		}
	}
	if v.briefScroll == nil {
		return 1
	}
	const cellWidth = 180
	width := v.briefScroll.Size().Width
	if width <= cellWidth {
		return 1
	}
	padding := theme.Padding()
	cols := int((width + padding) / (cellWidth + padding))
	if cols < 1 {
		cols = 1
	}
	return cols
}

// scrollBriefToRow positions Brief view's scroll offset so row's cell is
// roughly centered in the visible area, rather than just barely at the
// top/bottom edge.
func (v *fileListView) scrollBriefToRow(row int) {
	if v.briefScroll == nil {
		return
	}
	cols := v.currentBriefColumns()
	gridRow := row / cols
	padding := theme.Padding()
	y := float32(gridRow) * (briefCellHeight + padding)
	y -= v.briefScroll.Size().Height / 2
	if y < 0 {
		y = 0
	}
	v.briefScroll.ScrollToOffset(fyne.NewPos(0, y))
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

// rowColor returns the text color a row/cell should use given cursor/
// selection/type state. Only the active pane shows its cursor row in
// TextCursor — the inactive pane's cursor is drawn as normal text, so
// exactly one pane's cursor stands out at a time (classic dual-pane
// behavior) without needing a 5th "dimmed cursor" color. Cursor/selection
// take priority over the directory color, same as classic commanders.
func (v *fileListView) rowColor(cs ColorScheme, entry vfs.Entry) color.Color {
	if v.isActive() && v.state.Cursor == entry.Name {
		return cs.TextCursor
	}
	if v.state.Selected[entry.Name] {
		return cs.TextSelected
	}
	if entry.IsDir {
		return cs.TextDir
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
	onOtherKey      func(*fyne.KeyEvent)
	onSecondaryTap  func(*fyne.PointEvent)
	pendingModifier fyne.KeyModifier // Shift/Ctrl held on the click currently in flight — see MouseDown
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

// MouseDown/MouseUp implement desktop.Mouseable so a Shift/Ctrl-click's
// modifier state can be captured before Tapped fires: widget.Table's own
// Tapped/Select/OnSelected chain (which drives handleTableTap) only ever
// hands back a bare TableCellID, with no modifier info at all. The
// modifier is stashed in pendingModifier and consumed by handleTableTap
// immediately after, for exactly this one click.
func (t *keyTable) MouseDown(e *desktop.MouseEvent) {
	t.pendingModifier = e.Modifier
	t.Table.MouseDown(e)
}

func (t *keyTable) MouseUp(e *desktop.MouseEvent) {
	t.Table.MouseUp(e)
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

// prefColumnWidthKeys names each column's persisted width — resizing is a
// single, shared, app-wide setting (like showHiddenFiles/showDriveBar),
// not per-tab, matching how columnWidths itself is already a shared
// package-level array rather than per-view state.
var prefColumnWidthKeys = [tableColumnCount]string{
	colName:     "columnWidthName",
	colExt:      "columnWidthExt",
	colSize:     "columnWidthSize",
	colModified: "columnWidthModified",
	colPerm:     "columnWidthPerm",
}

// loadColumnWidths overwrites columnWidths' built-in defaults with
// whatever was persisted, if anything — called once at startup, before
// any pane/view exists, so their initial construction already reflects it.
func loadColumnWidths(a fyne.App) {
	for col, key := range prefColumnWidthKeys {
		columnWidths[col] = float32(a.Preferences().FloatWithFallback(key, float64(columnWidths[col])))
	}
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
			nameStack := container.NewStack(
				newHoverName(canvas.NewText("", color.White)),
				newRenameEntry(func(text string) { v.commitRename(text) }, v.cancelRename),
			)
			return container.NewStack(
				canvas.NewRectangle(color.Transparent),
				// Border, not HBox: HBox only ever gives nameStack its own
				// natural minimum width, which for the plain canvas.Text
				// label happened to look fine (Fyne doesn't clip text to its
				// layout box, so it visually overflowed into the rest of the
				// column anyway) but renders the renameEntry as a tiny box
				// once it's shown, since a real widget IS clipped to
				// whatever size it's given. Border's center slot stretches
				// to fill all remaining width after the checkbox.
				container.NewBorder(nil, nil, widget.NewCheck("", nil), nil, nameStack),
			)
		},
		v.updateCell,
		func(ev *fyne.KeyEvent) {
			if v.onOtherKey != nil {
				v.onOtherKey(ev)
			}
		},
		func(e *fyne.PointEvent) { v.offerContextMenu(v.state.Cursor, e.AbsolutePosition) },
	)
	t.SetColumnWidth(colName, columnWidths[colName])
	t.SetColumnWidth(colExt, columnWidths[colExt])
	t.SetColumnWidth(colSize, columnWidths[colSize])
	t.SetColumnWidth(colModified, columnWidths[colModified])
	t.SetColumnWidth(colPerm, columnWidths[colPerm])
	t.OnSelected = v.handleTableTap
	return t
}

// applyColumnWidth is commander.columnResized's per-view half: pushes a
// (already-updated in the shared columnWidths) column width into this
// view's actual Table, and re-lays-out its header row (labels + resize
// handles) to match. Called for every open tab in both panes so resizing
// a column in one pane's Full view is reflected everywhere immediately,
// since column widths are a single, shared, app-wide setting rather than
// per-tab.
func (v *fileListView) applyColumnWidth(col int, width float32) {
	if v.table != nil {
		v.table.SetColumnWidth(col, width)
	}
	if v.headerRow != nil {
		v.headerRow.Refresh()
	}
}

func (v *fileListView) updateCell(id widget.TableCellID, o fyne.CanvasObject) {
	stack := o.(*fyne.Container)
	bg := stack.Objects[0].(*canvas.Rectangle)
	border := stack.Objects[1].(*fyne.Container)
	// container.NewBorder orders Objects as [center..., left] when only a
	// left edge is set (see its doc comment) — nameStack first, check second.
	nameStack := border.Objects[0].(*fyne.Container)
	check := border.Objects[1].(*widget.Check)
	hn := nameStack.Objects[0].(*hoverName)
	txt := hn.txt
	renameField := nameStack.Objects[1].(*renameEntry)

	cs := v.colors()
	bg.FillColor = cs.PanelBG
	bg.Refresh()

	entry, ok := v.entryAt(id.Row)
	if !ok {
		txt.Text = ""
		hn.Show()
		renameField.Hide()
		check.Hidden = true
		txt.Refresh()
		return
	}

	// widget.Table recycles a fixed pool of cell objects across whichever
	// rows are currently scrolled into view — scrolling the renaming row
	// out of sight reassigns its renameField to a different row entirely,
	// with no FocusLost/Escape ever firing (scrolling isn't a focus change),
	// which would otherwise leave the rename stuck "open" against a row
	// that's no longer even on screen. Detecting the exact reassignment
	// here — this object was the active rename field, but is no longer
	// showing the row being renamed — is the one place that can catch it.
	if v.renaming != nil && v.activeRenameField == renameField && v.renaming.name != entry.Name {
		v.renaming = nil
		v.activeRenameField = nil
		if canvas := fyne.CurrentApp().Driver().CanvasForObject(v.table); canvas != nil {
			canvas.Unfocus()
		}
	}

	renaming := id.Col == colName && v.renaming != nil && v.renaming.name == entry.Name
	if renaming {
		v.activeRenameField = renameField
	}
	// renameField lives in the same nameStack template shared by every
	// column (only the checkbox is column-gated below), so every column
	// besides a Name cell mid-rename must explicitly keep it hidden — left
	// to its default visibility it would sit on top of, and blank out, the
	// Ext/Size/Modified/Perm text underneath.
	if renaming {
		hn.Hide()
		renameField.Show()
	} else {
		renameField.Hide()
		hn.Show()
	}

	txt.Color = v.rowColor(cs, entry)
	check.Hidden = id.Col != colName || entry.Name == parentEntryName || renaming

	switch id.Col {
	case colName:
		if renaming {
			break
		}
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
	if !renaming {
		hn.SetToolTip(txt.Text) // full, untruncated text — set before truncating below
		textSize := txt.TextSize
		if textSize == 0 {
			textSize = theme.TextSize()
		}
		maxWidth := columnWidths[id.Col] - columnTextMargin(id.Col, check)
		txt.Text = truncateToWidth(txt.Text, maxWidth, textSize, txt.TextStyle)
	}
	check.Refresh()
	txt.Refresh()
}

// columnTextMargin is how much of a column's width isn't available to its
// own text: colName shares its cell with the selection checkbox (see
// updateCell's Border layout), every column loses a little to padding on
// each side.
func columnTextMargin(col int, check *widget.Check) float32 {
	if col == colName {
		return check.MinSize().Width + theme.Padding()*2
	}
	return theme.Padding() * 2
}

// truncateToWidth ellipsizes text if it's wider than maxWidth — canvas.Text
// has no clipping/truncation of its own, so a name (or any other column's
// text) too wide for its column would otherwise just overflow, unclipped,
// into whatever's drawn to its right (see the "🐛 FIX" in 0.6.0's
// ReleaseNotes.txt entry this resolves).
func truncateToWidth(text string, maxWidth, textSize float32, style fyne.TextStyle) string {
	if maxWidth <= 0 {
		return text
	}
	full, _ := fyne.CurrentApp().Driver().RenderedTextSize(text, textSize, style, nil)
	if full.Width <= maxWidth {
		return text
	}
	runes := []rune(text)
	for i := len(runes) - 1; i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		sz, _ := fyne.CurrentApp().Driver().RenderedTextSize(candidate, textSize, style, nil)
		if sz.Width <= maxWidth {
			return candidate
		}
	}
	return "…"
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

	if v.renaming != nil && v.renaming.name != entry.Name {
		v.forceCancelRename()
	}

	mod := v.table.pendingModifier
	v.table.pendingModifier = 0 // one-shot: don't let it leak into a later keyboard-driven OnSelected

	// A double-click while Shift/Ctrl is held would mix "extend the
	// selection" with "activate" — simpler and less surprising to just skip
	// activation on a modified click.
	now := time.Now()
	isDouble := mod == 0 && id.Row == v.lastTapRow && now.Sub(v.lastTapTime) < doubleTapWindow
	// A second, slower click on the Name cell of a row that was ALREADY the
	// cursor (not just selected by this click) starts an inline rename —
	// classic Explorer/Finder "click, pause, click again" convention.
	wasCursor := mod == 0 && !isDouble && id.Col == colName && entry.Name != parentEntryName && v.state.Cursor == entry.Name
	v.lastTapRow, v.lastTapTime = id.Row, now

	v.applyClickSelection(entry.Name, mod)
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
		return
	}
	if wasCursor {
		v.beginInlineRename(entry)
	}
}

// applyClickSelection updates cursor/selection/anchor for a click on name,
// honoring Shift (range-select from the anchor) and Ctrl/Cmd (toggle just
// this row) as an alternative to checkbox multi-select. A plain click never
// touches Selected — unchanged from before shift/ctrl-click existed — so it
// can't accidentally clear a selection built up via the checkboxes.
func (v *fileListView) applyClickSelection(name string, mod fyne.KeyModifier) {
	shift := mod&fyne.KeyModifierShift != 0
	ctrl := mod&fyne.KeyModifierControl != 0 || mod&fyne.KeyModifierSuper != 0

	switch {
	case shift:
		anchor := v.selectAnchor
		if anchor == "" {
			anchor = name
		}
		if !ctrl {
			v.state.ClearSelection()
		}
		for _, n := range v.rangeBetween(anchor, name) {
			if n == parentEntryName {
				continue
			}
			v.state.Selected[n] = true
		}
	case ctrl:
		v.state.ToggleSelect(name)
		v.selectAnchor = name
	default:
		v.selectAnchor = name
	}
	v.state.Cursor = name
}

// rangeBetween returns every row's name between a and b (inclusive) in
// current display order, regardless of which one comes first — Shift-click
// works extending forward or backward from the anchor alike. A stale anchor
// (e.g. left over from a directory since navigated away from) falls back to
// just b, degrading to a plain single-row selection rather than erroring.
func (v *fileListView) rangeBetween(a, b string) []string {
	names := v.orderedNames()
	ia, ib := indexOfName(names, a), indexOfName(names, b)
	if ia < 0 || ib < 0 {
		return []string{b}
	}
	if ia > ib {
		ia, ib = ib, ia
	}
	return names[ia : ib+1]
}

func indexOfName(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
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
	n := v.rowCount()
	cells := make([]fyne.CanvasObject, 0, n)
	for row := 0; row < n; row++ {
		entry, ok := v.entryAt(row)
		if !ok {
			continue
		}
		cells = append(cells, v.buildBriefCell(entry, cs))
	}

	cols := 0
	if v.briefColumns != nil {
		cols = v.briefColumns()
	}
	var grid fyne.CanvasObject
	if cols > 0 {
		// A fixed column count: cells stretch to fill the available width
		// (see briefColumnsLayout) instead of wrapping at a fixed pixel
		// width, so the user's chosen count holds regardless of pane width.
		grid = container.New(&briefColumnsLayout{columns: cols}, cells...)
	} else {
		grid = container.NewGridWrap(fyne.NewSize(180, 28), cells...)
	}
	v.briefScroll = container.NewVScroll(grid)
	return v.briefScroll
}

// briefCellHeight is every Brief-view cell's fixed row height, in both Auto
// (GridWrap) and fixed-column (briefColumnsLayout) modes.
const briefCellHeight = 28

// briefColumnsLayout arranges Brief view cells into exactly Columns per row,
// with cell width recomputed from the available size on every layout pass
// (so a fixed column count holds across window/pane resizes) and a constant
// row height — unlike container.NewGridWithColumns, which stretches row
// height to fill the whole container and looks wrong for a handful of rows.
type briefColumnsLayout struct {
	columns int
}

func (b *briefColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	cols := b.columns
	if cols < 1 {
		cols = 1
	}
	padding := theme.Padding()
	cellWidth := (size.Width - float32(cols-1)*padding) / float32(cols)

	x, y := float32(0), float32(0)
	i := 0
	for _, child := range objects {
		if !child.Visible() {
			continue
		}
		child.Move(fyne.NewPos(x, y))
		child.Resize(fyne.NewSize(cellWidth, briefCellHeight))
		if (i+1)%cols == 0 {
			x = 0
			y += briefCellHeight + padding
		} else {
			x += cellWidth + padding
		}
		i++
	}
}

func (b *briefColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	cols := b.columns
	if cols < 1 {
		cols = 1
	}
	count := 0
	for _, child := range objects {
		if child.Visible() {
			count++
		}
	}
	rows := (count + cols - 1) / cols
	if rows < 1 {
		rows = 1
	}
	return fyne.NewSize(0, float32(rows)*briefCellHeight+float32(rows-1)*theme.Padding())
}

func (v *fileListView) buildBriefCell(entry vfs.Entry, cs ColorScheme) fyne.CanvasObject {
	name := entry.Name
	if v.renaming != nil && v.renaming.name == name {
		return v.buildBriefRenameCell(cs)
	}

	txt := canvas.NewText(name, v.rowColor(cs, entry))
	bg := canvas.NewRectangle(cs.PanelBG)
	content := container.NewStack(bg, container.NewPadded(txt))

	cell := newTappableCell(content, func(mod fyne.KeyModifier) {
		if v.onFocusGained != nil {
			v.onFocusGained()
		}
		if v.renaming != nil && v.renaming.name != name {
			v.forceCancelRename()
		}
		// A second, slower tap (Fyne only calls Tapped, not DoubleTapped, for
		// a genuine single/slow tap — see tappableCell's doc comment) on a
		// cell that was ALREADY the cursor starts an inline rename, same
		// convention as the Table view's handleTableTap.
		wasCursor := mod == 0 && entry.Name != parentEntryName && v.state.Cursor == name
		v.applyClickSelection(name, mod)
		v.reportSelection()
		if wasCursor {
			v.beginInlineRename(entry)
		} else {
			v.renderActiveView()
		}
	}, func() {
		if v.onFocusGained != nil {
			v.onFocusGained()
		}
		v.state.Cursor = name
		v.reportSelection()
		v.activateByName(name)
	}, func(e *fyne.PointEvent) {
		v.offerContextMenu(name, e.AbsolutePosition)
	})
	cell.fullText = name
	cell.truncText = txt
	cell.SetToolTip(name)
	return cell
}

// buildBriefRenameCell is buildBriefCell's rename-mode counterpart —
// unlike the Table view (whose cells are recycled and so need a
// permanently-present-but-hidden Entry), Brief view rebuilds its whole grid
// from scratch on every render (see renderActiveView), so it can simply
// swap in a real Entry for the cell being renamed.
func (v *fileListView) buildBriefRenameCell(cs ColorScheme) fyne.CanvasObject {
	bg := canvas.NewRectangle(cs.PanelBG)
	entryWidget := newRenameEntry(func(text string) { v.commitRename(text) }, v.cancelRename)
	v.activeRenameField = entryWidget
	return container.NewStack(bg, container.NewPadded(entryWidget))
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

// offerContextMenu is the right-click entry point for both view modes: the
// Table view acts on the current cursor row (see keyTable.TappedSecondary's
// doc comment for why), the Brief view's per-cell handler passes whatever's
// exactly under the pointer. ".." is excluded — none of the context menu's
// actions (open with, duplicate, trash, ...) make sense on the synthetic
// parent row.
func (v *fileListView) offerContextMenu(name string, pos fyne.Position) {
	if v.onContextMenu == nil || name == "" || name == parentEntryName {
		return
	}
	v.onContextMenu(name, pos)
}

// entryAndPath resolves a row name (as seen by offerContextMenu) to its
// vfs.Entry and full path within this view's current directory — the
// commander-side context menu (contextmenu_ui.go) needs both.
func (v *fileListView) entryAndPath(name string) (vfs.Entry, string, bool) {
	entry, ok := entryByName(v.entries, name)
	if !ok {
		return vfs.Entry{}, "", false
	}
	return entry, v.fs.Join(v.state.Path, entry.Name), true
}

func entryByName(entries []vfs.Entry, name string) (vfs.Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return vfs.Entry{}, false
}

// activate opens/navigates into entry — a directory (or "..") navigates,
// a plain file opens with the OS's default application. Two archive-aware
// exceptions: a .zip file (while browsing the real filesystem) is browsed
// into rather than opened externally, matching Nimble/Total Commander; a
// file already inside an open archive is extracted to a temp copy first
// (see commander.openArchivedMember in archive_browse_ui.go), since there
// is no real file at its presented path to hand the OS.
func (v *fileListView) activate(entry vfs.Entry) {
	if entry.Name == parentEntryName {
		v.navigateTo(v.fs.Dir(v.state.Path))
		return
	}
	if entry.IsDir {
		v.navigateTo(v.fs.Join(v.state.Path, entry.Name))
		return
	}
	fullPath := v.fs.Join(v.state.Path, entry.Name)
	if zfs, ok := v.fs.(*zipfs.FS); ok {
		if v.onOpenArchivedMember != nil {
			v.onOpenArchivedMember(zfs, entry.Name, fullPath)
		}
		return
	}
	if zipfs.HasZipExt(entry.Name) {
		v.enterZip(fullPath)
		return
	}
	openWithOS(fullPath)
}

// enterZip swaps this view onto a read-only vfs.FileSystem browsing the
// archive at zipPath, rooted at the archive's own path (see zipfs's doc
// comment for the "presented path" convention that keeps tabLabel/status
// line/Copy Path working unmodified).
func (v *fileListView) enterZip(zipPath string) {
	zfs, err := zipfs.Open(zipPath)
	if err != nil {
		if v.onStatus != nil {
			v.onStatus("cannot open archive: " + err.Error())
		}
		return
	}
	if !v.state.Navigate(zipPath) {
		zfs.Close()
		if v.onStatus != nil {
			v.onStatus("tab is locked")
		}
		return
	}
	v.fs = zfs
	v.reloadAfterNavigate()
}

// adjustFSForTarget swaps this view back onto the real filesystem the
// moment a navigation target (typically ".." from an open archive's own
// root, but equally a Home/Favorites jump) is no longer inside the
// currently-open archive — see zipfs.FS.Dir's doc comment for why this is
// the one place that needs to know about the swap at all.
func (v *fileListView) adjustFSForTarget(target string) {
	zfs, ok := v.fs.(*zipfs.FS)
	if !ok || zfs.IsInside(target) {
		return
	}
	zfs.Close()
	v.fs = localfs.New()
}

// closeFS releases this view's open archive handle, if it's currently
// browsing one — called when its tab closes (see paneview.go's
// CloseIntercept), since nothing else would ever navigate it back out.
func (v *fileListView) closeFS() {
	if zfs, ok := v.fs.(*zipfs.FS); ok {
		zfs.Close()
	}
}

// navigateTo is casual in-pane browsing (double-click/Enter into a
// subdirectory or ".."), which a locked tab may refuse — see JumpTo for
// explicit-destination navigation that a lock never blocks.
func (v *fileListView) navigateTo(target string) {
	v.adjustFSForTarget(target)
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
	v.adjustFSForTarget(target)
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

// NavigateUp goes to the current directory's parent — the same navigation
// activate() uses for double-clicking "..", exposed here for the pane's
// drive/volume toolbar's ".." button. Unlike JumpTo, this respects a
// locked tab (navigateTo does), matching what double-clicking ".." itself
// would do.
func (v *fileListView) NavigateUp() {
	v.navigateTo(v.fs.Dir(v.state.Path))
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

// SelectAll selects every real entry in the current listing (Ctrl/Cmd+A,
// and the pane toolbar's Select All/Deselect All button) — "..", if
// present, is never selectable, same as the per-row checkboxes.
func (v *fileListView) SelectAll() {
	for _, e := range v.entries {
		v.state.Selected[e.Name] = true
	}
	v.reportSelection()
	v.Refresh()
}

// DeselectAll clears the current selection (Ctrl/Cmd+Shift+A, and the pane
// toolbar's Select All/Deselect All button).
func (v *fileListView) DeselectAll() {
	v.state.ClearSelection()
	v.reportSelection()
	v.Refresh()
}

// HasSelection reports whether any row is currently selected — the pane
// toolbar's single Select All/Deselect All button uses this to decide which
// way to toggle next.
func (v *fileListView) HasSelection() bool {
	return len(v.state.Selected) > 0
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

// SelectionOrCursorNames is SelectionOrCursor's counterpart in terms of bare
// names rather than joined paths — F3 View needs the actual vfs.Entry (is
// it a directory? an archive? a file already inside one?) to decide how to
// view it, not just a path string.
func (v *fileListView) SelectionOrCursorNames() []string {
	var names []string
	for _, e := range v.entries {
		if v.state.Selected[e.Name] {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 && v.state.Cursor != "" && v.state.Cursor != parentEntryName {
		names = append(names, v.state.Cursor)
	}
	return names
}

// SelectionOrCursor returns full paths for the explicit multi-selection, or
// (if nothing is explicitly selected) just the cursor row — the rule F-key
// operations use to decide what they act on.
func (v *fileListView) SelectionOrCursor() []string {
	names := v.SelectionOrCursorNames()
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
// distinction). It also implements desktop.Mouseable purely to capture
// Shift/Ctrl state at click time — fyne.PointEvent (what Tapped receives)
// carries no modifier info at all, same reason keyTable does the same for
// the Table view.
type tappableCell struct {
	widget.BaseWidget
	ttwidget.ToolTipWidgetExtend // hover shows the full name — see buildBriefCell's SetToolTip call
	content                      fyne.CanvasObject
	onTap                        func(mod fyne.KeyModifier)
	onDoubleTap                  func()
	onSecondaryTap               func(*fyne.PointEvent)
	pendingModifier              fyne.KeyModifier

	// fullText/truncText, if set (buildBriefCell's name cells; left nil for
	// the rename cell's Entry, which handles its own text), re-ellipsize on
	// every resize — canvas.Text has no clipping of its own, and the Brief
	// grid's cell width varies (Auto's fixed 180px, or a fixed column count
	// stretched to the pane's current width), so truncating once at build
	// time isn't enough. Same technique as Full view's truncateToWidth.
	fullText  string
	truncText *canvas.Text
}

func newTappableCell(content fyne.CanvasObject, onTap func(fyne.KeyModifier), onDoubleTap func(), onSecondaryTap func(*fyne.PointEvent)) *tappableCell {
	c := &tappableCell{content: content, onTap: onTap, onDoubleTap: onDoubleTap, onSecondaryTap: onSecondaryTap}
	c.ExtendBaseWidget(c)
	c.ExtendToolTipWidget(c)
	return c
}

func (c *tappableCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

func (c *tappableCell) Resize(size fyne.Size) {
	c.BaseWidget.Resize(size)
	if c.truncText == nil {
		return
	}
	textSize := c.truncText.TextSize
	if textSize == 0 {
		textSize = theme.TextSize()
	}
	maxWidth := size.Width - theme.Padding()*2
	c.truncText.Text = truncateToWidth(c.fullText, maxWidth, textSize, c.truncText.TextStyle)
	c.truncText.Refresh()
}

func (c *tappableCell) MouseDown(e *desktop.MouseEvent) { c.pendingModifier = e.Modifier }
func (c *tappableCell) MouseUp(*desktop.MouseEvent)     {}

func (c *tappableCell) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		mod := c.pendingModifier
		c.pendingModifier = 0
		c.onTap(mod)
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

// hoverName wraps one Full-view cell's canvas.Text with hover-tooltip
// support (same ttwidget mechanism every toolbar button's tooltip already
// uses — see tappableCell for Brief view's equivalent), so hovering a
// truncated Name/Ext/Size/Modified/Perm cell shows its untruncated text.
// widget.Table owns Tapped/TappedSecondary at the whole-table level (see
// keyTable), never routing mouse events to individual cell content — but
// hover (desktop.Hoverable) is resolved by the driver via a plain
// position-based hit-test, entirely independent of that, so it reaches this
// wrapper anyway despite Table's own click handling bypassing it.
type hoverName struct {
	widget.BaseWidget
	ttwidget.ToolTipWidgetExtend
	txt *canvas.Text
}

func newHoverName(txt *canvas.Text) *hoverName {
	h := &hoverName{txt: txt}
	h.ExtendBaseWidget(h)
	h.ExtendToolTipWidget(h)
	return h
}

func (h *hoverName) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.txt)
}
