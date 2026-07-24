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
• Swap Panes (Ctrl+U, or the popup menu) exchanges the left and right panes'
  entire tab contents — paths, locks, view mode, sort, selection — at once.

VIEW MODES & SORTING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Brief: a compact, name-only view wrapped into as many columns as fit.
• Full: adds sortable Name / Ext / Size / Modified / Permissions columns —
  click a header to sort by it, click again to reverse. Sorting by
  Extension breaks ties by name, so files group by type and then
  alphabetically within each type.
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
                          Folder Sizes, Search, Show Hidden Files, Panel
                          Colors, Editors, 7-Zip Binary Path, Help, About.
F10 Quit                  Quits ` + appName + `.
Enter                     Opens/navigates into the cursor row, same as a
                          double-click.
Double-click               A directory navigates into it; a file opens with
                          your OS's default application — unless it's an
                          executable, which launches directly and detached
                          (it keeps running after you quit, and won't get
                          wrapped in a Terminal window on macOS).

SELECTING FILES:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Click a row to move the cursor there; use the checkbox beside a Name to
  add it to the multi-selection used by Copy/Move/Delete/Compress. With
  nothing explicitly selected, these operations act on just the cursor row.
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
as "name copy", "name copy 2", …); Move to Trash; Copy Name / Copy Path
(to the clipboard, as text); Compress (To .zip, always available, or To
.7z — see COMPRESSING below); Create Symbolic Link… (defaults to the
opposite pane's directory, same name); Reveal in File Manager (opens
Finder/Explorer/your Linux file manager with the item selected); Reveal in
Opposite Pane / Reveal in Opposite Pane (New Tab); and, for directories,
Add to Favorites. Compress acts on the whole current selection (or just the
cursor row); everything else here acts on whichever row you right-clicked.

COMPRESSING:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
.zip needs nothing extra. .7z only appears as an option when a 7z-capable
binary (7z, 7za, or 7zz) is found on your PATH, or one you've pointed at
explicitly via File → 7-Zip Binary Path… — there's no bundled .7z writer.

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
