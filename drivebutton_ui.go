// drivebutton_ui.go — the volume/drive toolbar's per-drive button: a plain
// tap navigates there (see paneview.go's buildDriveBarContent), a
// right-click offers Eject/Format (internal/driveops). Neither
// ttwidget.Button nor the underlying widget.Button supports a secondary
// tap, so this is a small extension of it, the same technique rename_ui.go
// uses for renameEntry (extending widget.Entry).
package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"commander/internal/driveops"
)

type driveButton struct {
	ttwidget.Button
	onSecondary func(pos fyne.Position)
}

func newDriveButton(label string, onTapped func(), onSecondary func(pos fyne.Position)) *driveButton {
	b := &driveButton{onSecondary: onSecondary}
	b.Text = label
	b.OnTapped = onTapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *driveButton) TappedSecondary(e *fyne.PointEvent) {
	if b.onSecondary != nil {
		b.onSecondary(e.AbsolutePosition)
	}
}

// showDriveContextMenu is a drive button's right-click menu: Eject (when
// supported on this platform, and not the boot volume — see
// driveops.IsBootVolume) and Format… (opens the native disk-management
// tool; see internal/driveops's doc comment for why it never auto-selects
// a specific drive).
func (p *pane) showDriveContextMenu(root string, pos fyne.Position) {
	if !driveops.Supported() {
		return
	}

	var items []*fyne.MenuItem
	if !driveops.IsBootVolume(root) {
		items = append(items, fyne.NewMenuItem("Eject", func() {
			if err := p.onEject(root); err != nil {
				dialog.ShowError(err, p.win)
			}
		}))
	}
	items = append(items, fyne.NewMenuItem("Open Disk Utility…/Disk Management…", func() {
		if err := driveops.OpenFormatTool(); err != nil {
			dialog.ShowError(err, p.win)
		}
	}))

	menu := fyne.NewMenu("", items...)
	widget.NewPopUpMenu(menu, p.win.Canvas()).ShowAtPosition(pos)
}

// ejectDrive is every pane's onEject callback: navigate any tab in EITHER
// pane that's currently browsing inside root back to a safe default
// first — TotalCmd does the same, rather than leaving a pane stranded on
// a directory that's about to disappear — then eject, then re-scan both
// drive bars, since the available-volumes list just changed for both.
func (c *commander) ejectDrive(root string) error {
	navigateOffRoot(c.left, root)
	navigateOffRoot(c.right, root)

	if err := driveops.Eject(root); err != nil {
		return err
	}

	c.left.rescanDriveBar()
	c.right.rescanDriveBar()
	return nil
}

// navigateOffRoot jumps every tab in p that's currently inside root (or is
// root itself) to p's default home, so ejecting root doesn't leave that
// tab pointing at a directory that's about to vanish.
func navigateOffRoot(p *pane, root string) {
	trimmedRoot := strings.TrimRight(root, `/\`)
	for _, v := range p.views {
		current := v.CurrentPath()
		if current != root && current != trimmedRoot &&
			!strings.HasPrefix(current, trimmedRoot+"/") && !strings.HasPrefix(current, trimmedRoot+`\`) {
			continue
		}
		v.JumpTo(p.defaultHome())
	}
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
