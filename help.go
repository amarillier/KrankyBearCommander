package main

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var helpWindow fyne.Window

// showHelp displays comprehensive help documentation
// Reusable pattern from KrankyBearClock - customize these for your app:
//   - appName: Your application name
//   - resourceKrankyBearCommanderPng: Your embedded icon resource
//   - helpText: Your application's help content (see below for structure)
//   - GitHub and License URLs
//
// Help text structure recommendation:
//   - Use section headers with visual separators (━━━)
//   - Group related features together
//   - Include tips, tricks, and known limitations
//   - Add keyboard shortcuts
//   - Provide links to external resources
func showHelp(a fyne.App) {
	if helpWindow != nil && helpWindow.Content().Visible() {
		helpWindow.Show()
		helpWindow.RequestFocus()
		return
	}

	helpWindow = a.NewWindow(appName + " - Help")
	helpWindow.SetIcon(resourceKrankyBearCommanderPng)

	helpText := `` + appName + ` - Help

OVERVIEW:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
A free, cross-platform dual-pane file manager in the spirit of Norton
Commander / Total Commander / Nimble Commander / Midnight Commander. Two
panes, each with its own tabs, browse independently; the classic F-key row
along the bottom drives every file operation.

PANES & TABS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Click a pane (or a row inside it) to make it the active pane — the active
  pane's cursor row is highlighted; F-key operations act on it, and Copy/Move
  target the OTHER pane's current directory.
• "+" on a tab strip opens a new tab; the × on a tab closes it (at least one
  tab per pane always stays open).
• 🔓/🔒 locks a tab to its current directory. Locking asks whether you can
  still open subdirectories from there: if allowed, Home/\/ / always snap
  back to the locked directory instead of going further; if not, the tab is
  fully pinned and directory changes are refused.
• ⌂ (Home) goes to the locked directory (if locked) or your home directory.
• If a tab's current directory has genuinely vanished (e.g. an unmounted/
  disconnected drive), it jumps back to your home directory automatically
  instead of getting stuck. A locked tab's lock is unaffected — Home still
  returns to the original locked location if the drive is reconnected.
• Swap Panes (Ctrl+U, or the popup menu) exchanges the left and right panes'
  entire tab contents — paths, locks, view mode, sort, selection — at once.

VIEW MODES & SORTING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Brief: a compact, name-only view wrapped into as many columns as fit
  (or a fixed 2/3/4-column count — View menu / F9 popup → Brief Columns).
  Text too long for its cell is ellipsized (…), same as Full view.
• Full: adds sortable Name / Ext / Size / Modified / Permissions columns —
  click a header to sort by it, click again to reverse. Sorting by
  Extension breaks ties by name, so files group by type and then
  alphabetically within each type. Drag the boundary right after a
  column's header to resize it — applies to every open tab in both panes
  at once and persists across launches, since column widths are one
  shared setting, not per-tab. Text too long for its column is ellipsized
  (…) rather than overflowing into whatever's next to it.
• Directories always sort before files, and ".." (parent) always comes
  first when the tab isn't already at its filesystem root.

FUNCTION KEYS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
F1  Help                 This window.
F2  Refresh               Re-reads the active pane's directory from disk —
                          useful if something else changed it on disk while
                          the tab sat open.
F3  View                 Read-only viewer — text, or a hex dump for
                          anything that looks binary.
F4  Edit                 Opens the built-in text editor, or your chosen
                          default external editor (see EDITORS below).
F5  Copy                 Copies the selection (or the cursor item, if
                          nothing's explicitly selected) to the other pane's
                          directory.
F6  Move / Rename        Multiple items move to the other pane's directory;
                          a single item shows an editable path — change the
                          name for a rename, the directory for a move, or
                          both at once.
F7  MkDir                 Creates a new folder in the active pane, prefilled
                          with the cursor row's name so retyping part of it
                          is quick.
F8  Delete                Sends the selection to the trash.
⇧F8 Delete Permanently    Bypasses the trash — cannot be undone. Mouse/menu
                          only (see KNOWN LIMITATIONS).
F9  Menu                  New tab, view mode, Refresh, Swap Panes, Calculate
                          Folder Sizes, Search, Copy/Paste, Multi-Rename
                          Tool, Show Hidden Files, Show Volume/Drive
                          Toolbar, Panel Colors, Editors, 7-Zip Binary
                          Path, Help, Check for Updates, About.
F10 Quit                  Quits ` + appName + `.
Enter                     Opens/navigates into the cursor row, same as a
                          double-click.
Double-click               A directory navigates into it; a file opens with
                          your OS's default application — unless it's an
                          executable, which launches directly and detached
                          (it keeps running after you quit, and won't get
                          wrapped in a Terminal window on macOS).

VOLUME/DRIVE TOOLBAR:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
A row above each pane's tabs: \ (home), .. (up one level), a refresh
button (same as F2, and also re-scans for newly connected drives), then
one button per filesystem root — drive letters on Windows, or "/" plus
any mounted external volume (USB drive, SD card) on macOS/Linux.
Scrollable, so a machine with many drives doesn't force the pane wider.
Shown by default; toggle via View menu or F9 popup ("Show Volume/Drive
Toolbar"). Right-click a drive button for Eject (hidden for the boot
volume) or to open the OS's native disk-management tool (Disk Utility /
Disk Management) — the latter doesn't auto-select any particular drive,
you choose it yourself in that tool, same as always.

SELECTING FILES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Click a row to move the cursor there; use the checkbox beside a Name to
  add it to the multi-selection used by Copy/Move/Delete/Compress. With
  nothing explicitly selected, these operations act on just the cursor row.
• Click an already-selected/cursor row's name again — slower than a
  double-click, which still opens/navigates as usual — to rename it in
  place. Enter commits, Escape or clicking away cancels; the extension is
  hidden while editing and silently reattached unless you type your own.
• Shift-click selects every row between the anchor (the last plain- or
  Ctrl/Cmd-clicked row) and the one you click, replacing the current
  selection — an alternative to the checkboxes for selecting many items at
  once. Ctrl-click (⌘-click on macOS) toggles just the clicked row and moves
  the anchor there, so a following Shift-click extends from it; Shift+Ctrl-
  click adds a range to the existing selection instead of replacing it.
• Select All (Ctrl+A / ⌘A) and Deselect All (Ctrl+Shift+A / ⌘⇧A), or the ☑
  toolbar button, which toggles between the two based on whether anything's
  currently selected.

RIGHT-CLICK MENU:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Right-click any file or directory row for: Open; Open With (your configured
external editors, see EDITORS below); Duplicate (copies it alongside itself
as "name copy", "name copy 2", …); Move to Trash; Copy (real files, to the
OS clipboard — see COPY/PASTE & DRAG-IN below) / Paste (into this
directory) / Copy Name / Copy Path (as text); Compress (To .zip, always
available, or To .7z — see COMPRESSING below); Create Symbolic Link…
(defaults to "link-<name>" alongside the source, name pre-selected);
Reveal in File Manager (opens Finder/Explorer/your Linux file manager with
the item selected); Reveal in Opposite Pane / Reveal in Opposite Pane (New
Tab); and, for directories, Add to Favorites. Compress acts on the whole
current selection (or just the cursor row); Paste acts on this row's
directory regardless of which row you right-clicked; everything else here
acts on whichever row you right-clicked.
Inside an open archive this menu is much shorter — see BROWSING ARCHIVES.

MULTI-RENAME TOOL:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Ctrl+M (or the right-click menu / File menu / F9 popup) opens a
TotalCmd-style batch rename for the selection (or just the cursor row),
with a live old→new preview before anything actually changes on disk:
• Pattern placeholders: [N] original name, [N1-3] characters 1-3 of the
  name, [E] extension, [C] a counter ([C:start], [C:start,step],
  [C:start,step,width] for zero-padding). An empty pattern defaults to
  [N]. If your pattern doesn't include [E], the original extension is
  kept automatically.
• Case: No Change / UPPERCASE / lowercase / Title Case / Sentence case,
  applied to the whole computed name.
• Find/Replace: plain text or a regular expression.
Renaming two files to each other's names (or any other same-batch
collision) is handled safely; a new name that would collide with some
other, unrelated existing file is refused before anything is touched.
Reopens with whatever Pattern/Case/Find/Replace/Regex you used last time
already filled in, rather than resetting to defaults.
After a successful rename, the selection is cleared (since renamed items no
longer match it); a rejected batch leaves your selection as-is to retry.

COMPRESSING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
.zip needs nothing extra. .7z only appears as an option when a 7z-capable
binary (7z, 7za, or 7zz) is found on your PATH, or one you've pointed at
explicitly via File → 7-Zip Binary Path… — there's no bundled .7z writer.

BROWSING ARCHIVES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Double-click a .zip to browse straight into it, exactly like a real
directory — tabs, sorting, view modes, and selection all work the same
way. ".." at the archive's own root steps back out to the real folder it
lives in. An archive is read-only: F5 extracts the selection (or just the
cursor row) to the opposite pane's directory instead of copying, and
F4 Edit, Move/Rename, MkDir, and Delete all refuse with a dialog.
F3 View extracts a single file to a temp copy first, then opens it in the
normal viewer; F3 on a .zip you haven't browsed into yet offers a lightweight
picker to preview one member without fully browsing in.

COPY/PASTE & DRAG-IN/OUT:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Ctrl+C / ⌘C copies the selection (or cursor row) to the OS clipboard as
real files — Paste into Finder/Explorer/Nautilus copies them there for
real, exactly as if you'd copied the files themselves. Ctrl+V / ⌘V does
the reverse: copies whatever files are currently on the OS clipboard (e.g.
from Ctrl+C on files in Finder/Explorer) into the active pane's directory.
Also available as Copy on the right-click menu, and Copy/Paste on the File
menu and F9 popup. You can also just drag files from Finder/Explorer/
Nautilus and drop them onto a pane to copy them in.

On macOS and Windows, you can also drag files OUT of a pane — onto
Finder/Explorer, the opposite pane, or any other app that accepts dropped
files (e.g. Slack, Mail) — the same way you'd drag them from Finder or
Explorer itself. Click-dragging carries whichever files are currently
selected (or the cursor row) in the pane the drag started in. Not yet
available on Linux.

FAVORITES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
The ★ button (one shared list, available from either pane) lists your
filesystem's volumes plus your bookmarked directories — pick one to jump the
active tab there. Right-click any directory to bookmark it directly, or use
"Add Current Directory…" / "Manage Favorites…" from the ★ menu. Seeded on
first run with common folders for your OS (Desktop, Downloads, and
Applications on macOS).

EDITORS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
F4 opens whichever editor is currently the default: the built-in editor, or
one of any number of external editors you configure (a name plus the
command to launch — the file path is appended as its last argument).
Change the default, or add/remove external editors, from F9 → Editors.

PANEL COLORS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
The pane colors (background, normal/selected/cursor-row/directory text)
default to a Norton-Commander-style scheme and are fully customizable —
F9 → Panel Colors, or View → Panel Colors — independent of the
Light/Dark/System app theme (View menu), which governs the rest of the
app's chrome. Directories (and "..") use their own color so they stand out
from ordinary files at a glance.

HIDDEN FILES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Dotfiles are hidden by default. Toggle Show Hidden Files from the View menu
or F9 popup — the choice applies to both panes and persists across
launches.

FOLDER SIZES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Calculate Folder Sizes (File menu / F9 popup) walks every directory in the
active pane's listing and fills in its real recursive size where the Size
column otherwise just shows "<DIR>", plus the current directory's own
total on the ".." row. Runs in the background with a cancelable progress
dialog.

SEARCH:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
The 🔍 toolbar button (or File menu / F9 popup → Search…) recursively
searches the active tab's directory by plain substring or a */? wildcard
pattern. Picking a match from the results list opens its location in a new
tab with the file selected as the cursor.

SMART FEATURES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✨ Tab/pane/window layout, panel colors, favorites, editor choice, hidden-
   files visibility, and the 7-Zip binary path all persist across launches.
✨ Copy/Move run in the background with a progress dialog and
   Overwrite/Skip/Rename/Cancel conflict handling (with "apply to all").
✨ Theme Support: Light, Dark, or System theme (View menu) - matches your
   preference.
✨ Tooltips on every button explain what it does.

KEYBOARD SHORTCUTS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• F1-F10 - see FUNCTION KEYS above.
• Ctrl+U - Swap Panes.
• Ctrl+A / ⌘A - Select All. Ctrl+Shift+A / ⌘⇧A - Deselect All.
• Ctrl+C / ⌘C - Copy (real files, to the OS clipboard). Ctrl+V / ⌘V - Paste.
• Ctrl+M - Multi-Rename Tool. Literal Ctrl even on macOS (not ⌘), since
  ⌘M is already macOS's own Minimize Window shortcut below.
• Shift-click / Ctrl-click (⌘-click on macOS) - see SELECTING FILES above.
• Enter - Open/navigate into the cursor row.
• Cmd/Ctrl+Q - Quit
• Cmd/Ctrl+W - Close window
• Cmd/Ctrl+M - Minimize

KNOWN LIMITATIONS:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Shift+F8 (permanently delete) is mouse/menu-only — a Fyne limitation means
  key events don't carry modifier state, so it can't be told apart from
  plain F8 via the keyboard. Fitting, really, for a "bypass the trash"
  action.
• Arrow-key row navigation and the right-click menu are most precise in
  Full view; Brief view's per-cell right-click is exact, but Full view's
  context menu acts on the current cursor row rather than pixel-precise
  position — left-click a row first, then right-click anywhere on the
  table for its context menu.
• On macOS, F2 may be mapped to a hardware brightness key by default —
  either hold Fn, or enable "Use F1, F2, etc. as standard function keys"
  in System Settings → Keyboard, to use it for Refresh.

MORE INFORMATION:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
For documentation, bug reports, or feature requests:
📦 GitHub: https://github.com/amarillier/KrankyBearCommander
📄 License: https://github.com/amarillier/KrankyBearCommander/blob/main/LICENSE
📝 Release Notes: Check "Help → Check for Updates"

FREE SOFTWARE - Use anywhere, anytime, any purpose!
No registration, no tracking, no phone-home (except manual update checks).
`

	helpLabel := widget.NewLabel(helpText)
	helpLabel.Wrapping = fyne.TextWrapWord

	// Links - update URLs for your project
	githubURL, _ := url.Parse("https://github.com/amarillier/KrankyBearCommander")
	githubLink := widget.NewHyperlink("Visit GitHub Repository", githubURL)
	githubLink.Alignment = fyne.TextAlignCenter

	licenseURL, _ := url.Parse("https://github.com/amarillier/KrankyBearCommander/blob/main/LICENSE")
	licenseLink := widget.NewHyperlink("View License", licenseURL)
	licenseLink.Alignment = fyne.TextAlignCenter

	// Create scrollable area with minimum size for better readability
	scrollContent := container.NewScroll(helpLabel)
	scrollContent.SetMinSize(fyne.NewSize(750, 550))

	// Layout with better proportions
	header := container.NewVBox(
		widget.NewLabelWithStyle(appName+" - Help", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
	)

	footer := container.NewVBox(
		widget.NewSeparator(),
		container.NewCenter(container.NewHBox(githubLink, licenseLink)),
	)

	content := container.NewBorder(header, footer, nil, nil, scrollContent)

	helpWindow.SetContent(container.NewPadded(content))
	helpWindow.Resize(fyne.NewSize(850, 700))

	helpWindow.SetCloseIntercept(func() {
		helpWindow.Hide()
	})

	helpWindow.Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
