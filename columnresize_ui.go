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
// resize uses.
type columnResizeHandle struct {
	widget.BaseWidget
	col int
}

func newColumnResizeHandle(col int) *columnResizeHandle {
	h := &columnResizeHandle{col: col}
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

// resizeHandleLayout positions each handle centered on the boundary right
// after its column, independent of the header row's own columnsLayout
// (this is a separate overlay stacked on top of it — see
// fileListView.Build) — so adding resize handles doesn't require changing
// the header row's own object list/layout at all.
type resizeHandleLayout struct{}

const resizeHandleWidth = 6

func (resizeHandleLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0) // doesn't influence the Stack's own size — the real header row does
}

func (resizeHandleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	var x float32
	for i, o := range objects {
		x += columnWidths[i]
		o.Resize(fyne.NewSize(resizeHandleWidth, size.Height))
		o.Move(fyne.NewPos(x-resizeHandleWidth/2, 0))
		x += pad
	}
}

// resizeColumn is every columnResizeHandle's Dragged/DragEnd: updates the
// shared columnWidths (clamped to a sane minimum so a column can't be
// dragged into uselessness), propagates the new width to every open
// tab/pane's table and header immediately, and persists it once the drag
// finishes (final) rather than on every pixel of movement.
func resizeColumn(col int, dx float32, final bool) {
	w := columnWidths[col] + dx
	if w < minColumnWidth {
		w = minColumnWidth
	}
	columnWidths[col] = w
	if cmdr != nil {
		cmdr.columnResized(col, w, final)
	}
}
