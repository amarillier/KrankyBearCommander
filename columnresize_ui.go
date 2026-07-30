// columnresize_ui.go — drag-to-resize handles for Full view's column
// header. widget.Table has its own built-in column-resize-by-drag, but
// it's unusable here for two reasons: this app renders its own custom
// sort-button header row (columnsLayout in filelist.go), not Table's
// built-in header, so Table's own boundary-hover detection never
// activates; and even if it did, Table exposes no public getter for a
// column's current width after a resize (only SetColumnWidth, a setter) —
// there'd be no way to persist it or use it for text truncation. Doing it
// ourselves on our own header means we're always the source of truth for
// each column's width.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const minColumnWidth = 40

// columnResizeHandle is a thin draggable strip sitting on the boundary
// right after a column, resizing that column as it's dragged — the same
// left-edge-of-the-next-column convention widget.Table's own built-in
// resize uses. Double-clicking it (without dragging) auto-fits the column
// instead, Excel-style — contentWidth reports that column's widest current
// text (fileListView.columnContentWidth), which the handle itself has no
// way to know since it doesn't hold any listing data.
type columnResizeHandle struct {
	widget.BaseWidget
	col          int
	contentWidth func() float32
}

func newColumnResizeHandle(col int, contentWidth func() float32) *columnResizeHandle {
	h := &columnResizeHandle{col: col, contentWidth: contentWidth}
	h.ExtendBaseWidget(h)
	return h
}

func (h *columnResizeHandle) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	return widget.NewSimpleRenderer(rect)
}

func (h *columnResizeHandle) Cursor() desktop.Cursor {
	return desktop.HResizeCursor
}

func (h *columnResizeHandle) Dragged(e *fyne.DragEvent) {
	resizeColumn(h.col, e.Dragged.DX, false)
}

func (h *columnResizeHandle) DragEnd() {
	resizeColumn(h.col, 0, true)
}

// DoubleTapped only fires when the click didn't move past the drag
// threshold (see the glfw driver: Tapped/DoubleTapped are skipped entirely
// once a drag has started), so this and Dragged/DragEnd never fire for the
// same click.
func (h *columnResizeHandle) DoubleTapped(*fyne.PointEvent) {
	if h.contentWidth == nil {
		return
	}
	autoFitColumn(h.col, h.contentWidth())
}

// resizeHandleLayout positions each handle centered on the boundary right
// after its column, independent of the header row's own columnsLayout
// (this is a separate overlay stacked on top of it — see
// fileListView.Build) — so adding resize handles doesn't require changing
// the header row's own object list/layout at all. nameExtra mirrors
// columnsLayout's own field so colName's boundary lines up with the Name
// column's actual (possibly stretched) width — see fileListView.nameStretch.
type resizeHandleLayout struct {
	nameExtra func() float32
}

const resizeHandleWidth = 6

func (resizeHandleLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0) // doesn't influence the Stack's own size — the real header row does
}

func (rl resizeHandleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	var x float32
	for i, o := range objects {
		w := columnWidths[i]
		if i == colName && rl.nameExtra != nil {
			w += rl.nameExtra()
		}
		x += w
		o.Resize(fyne.NewSize(resizeHandleWidth, size.Height))
		o.Move(fyne.NewPos(x-resizeHandleWidth/2, 0))
		x += pad
	}
}

// setColumnWidth is resizeColumn's and autoFitColumn's shared tail: clamps
// to a sane minimum so a column can't shrink into uselessness, updates the
// shared columnWidths, and propagates the new width to every open tab/pane's
// table and header (persisting it only once final, so a drag doesn't write
// preferences on every pixel of movement).
func setColumnWidth(col int, w float32, final bool) {
	if w < minColumnWidth {
		w = minColumnWidth
	}
	columnWidths[col] = w
	if cmdr != nil {
		cmdr.columnResized(col, w, final)
	}
}

// resizeColumn is every columnResizeHandle's Dragged/DragEnd.
func resizeColumn(col int, dx float32, final bool) {
	setColumnWidth(col, columnWidths[col]+dx, final)
}

// autoFitColumn is a columnResizeHandle's DoubleTapped: sets col's width to
// its content (contentWidth, from fileListView.columnContentWidth) plus
// whatever that column doesn't give its text (checkbox/padding — see
// columnTextMargin), Excel's double-click-the-boundary convention. Always
// final — there's no in-progress drag to defer persisting until.
func autoFitColumn(col int, contentWidth float32) {
	setColumnWidth(col, contentWidth+columnTextMargin(col, widget.NewCheck("", nil)), true)
}
