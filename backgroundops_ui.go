// backgroundops_ui.go — "Background Operations…" (File menu / tray): lists
// every Copy/Move/Extract/Compare-Sync operation the user has sent to the
// background via its progress dialog's "Background" button (fileops_ui.go's
// runFileOp). Same list-dialog shape as showConnections/showManageFavorites/
// showLauncherMenu (connections_ui.go, favorites_ui.go, launcher_ui.go): a
// mutable container.NewVBox() rebuilt by a refresh() closure, inside a
// container.NewVScroll, hosted in a dialog.NewCustom.
package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// backgroundOp is one operation running in the background — still going in
// its own goroutine exactly as before runFileOp hid its dialog, just with
// nowhere to show its progress until this list is opened. status/value are
// cached here so opening this list reflects current state immediately;
// both are only ever touched via fyne.Do (the same discipline every other
// mutable UI field in this codebase already follows), so no extra locking
// is needed.
type backgroundOp struct {
	description string
	status      string
	value       float64 // 0..1; left untouched (stays at its last value) for whole-item rename-fast-path progress reports, which report done=total=0
	cancel      func()

	// srcView/dstView pin the two fileListViews this operation's Reload
	// calls target (captured once at launch in runFileOp, not re-resolved
	// at completion) — also consulted by commander.isPinnedByBackgroundOp
	// to refuse closing either tab while this operation is still running.
	srcView, dstView *fileListView
}

// showBackgroundOperations lists every currently-backgrounded operation,
// refreshed every 500ms while open (a time.Ticker, fyne.Do-wrapped, stopped
// via SetOnClosed) so progress visibly ticks rather than needing to close
// and reopen the dialog to see anything move.
func (c *commander) showBackgroundOperations() {
	list := container.NewVBox()
	var refresh func()

	refresh = func() {
		var rows []fyne.CanvasObject
		if len(c.backgroundOps) == 0 {
			rows = append(rows, widget.NewLabel("No background operations running."))
		}
		for _, op := range c.backgroundOps {
			statusLbl := widget.NewLabel(op.status)
			progressBar := widget.NewProgressBar()
			progressBar.SetValue(op.value)
			cancelBtn := widget.NewButton("Cancel", op.cancel)
			info := container.NewVBox(widget.NewLabelWithStyle(op.description, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), statusLbl, progressBar)
			rows = append(rows, container.NewBorder(nil, nil, nil, cancelBtn, info))
		}
		list.Objects = rows
		list.Refresh()
	}
	refresh()

	content := container.NewVScroll(list)
	d := dialog.NewCustom("Background Operations", "Close", content, c.win)
	d.Resize(fyne.NewSize(520, 360))

	ticker := time.NewTicker(500 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				fyne.Do(refresh)
			case <-done:
				return
			}
		}
	}()
	d.SetOnClosed(func() {
		ticker.Stop()
		close(done)
	})

	showDialog(d)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
