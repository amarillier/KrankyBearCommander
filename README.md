# KrankyBear Commander

A free, cross-platform dual-pane file manager for Windows, macOS, and Linux —
built with Go and the [Fyne](https://fyne.io/) GUI toolkit, in the spirit of
Norton Commander / Total Commander / Nimble Commander / Midnight Commander.

Design philosophy aligns with Fyne: ease of use, solid functionality, steady bug
fixing and performance work.

## Features

- **Dual panes, multiple tabs per pane** — each tab keeps its own directory,
  view mode, sort, and selection. Tabs can be **locked** to a directory, with
  a choice of whether subdirectories may still be opened (Home/`\`/`/` always
  snap back to the locked root). If a tab's directory genuinely vanishes
  (e.g. an unmounted drive), it jumps back to your home directory instead of
  getting stuck — a lock itself is unaffected, so Home still finds the
  original location again once it's back.
- **Brief and Full (detailed) views** — Brief is a compact, name-only wrapped
  grid, either auto-fit or a fixed 2/3/4-column count (View menu / F9 popup
  → Brief Columns); Full adds sortable, resizable Name / Ext / Size /
  Modified / Permissions columns (click a header to sort, click again to
  reverse; drag the boundary after a header to resize — applies everywhere
  and persists). Both views ellipsize names too long for their column/cell
  instead of overflowing into whatever's next to them, and show the full
  name in a tooltip on hover. Directories (and
  `..`) get their own configurable text color, distinct from files.
- **Classic F-key row**: F1 Help, F2 Refresh, F3 View, F4 Edit, F5 Copy, F6
  Move/Rename, F7 MkDir, F8 Delete (to trash), Shift+F8 delete permanently,
  F9 menu, F10 Quit — both as on-screen buttons and real keyboard shortcuts,
  with tooltips on every button. Escape cancels/closes whichever dialog is
  open, anywhere in the app, same as its own Cancel/No/Close button.
- **Selecting files**: checkboxes, Shift-click range-select, Ctrl/Cmd-click
  toggle, and Select All/Deselect All (Ctrl+A / Ctrl+Shift+A, or the ☑
  toolbar button). Click an already-selected row's name again (slower than
  a double-click) to **rename it in place**.
- **Right-click context menu**: Open, Open With (your configured external
  editors), Duplicate, Move to Trash, Copy/Paste (real files, to/from the OS
  clipboard) or Copy Name/Path (as text), Compress (to .zip, or .7z if a
  7z-capable binary is available), Multi-Rename Tool…, Create Symbolic
  Link…, Reveal in File Manager, Reveal in Opposite Pane, and Add to
  Favorites for directories.
- **Multi-Rename Tool** (Ctrl+M) — TotalCmd-style batch rename with pattern
  placeholders (`[N]` name, `[N1-3]` name characters, `[E]` extension,
  `[C]` counter with start/step/zero-padding), whole-name case conversion,
  and find/replace (plain text or regex), with a live old→new preview
  before anything changes on disk.
- **Native OS clipboard and drag-in**: Ctrl/Cmd+C copies the selection as
  real files — Paste into Finder/Explorer/Nautilus copies them there for
  real; Ctrl/Cmd+V does the reverse. Implemented as native platform code
  (Cocoa/Win32/xclip) rather than osascript or PowerShell. You can also drag
  files from your OS file manager and drop them onto a pane to copy them in.
- **Drag-out (macOS and Windows)**: click-drag the current selection (or
  cursor row) out of a pane onto Finder/Explorer, the opposite pane, or any
  other app that accepts dropped files (e.g. Slack, Mail) — a native Cocoa
  drag session on macOS, real OLE COM objects via `DoDragDrop` on Windows;
  neither uses osascript or PowerShell. Linux support is planned but not
  yet available.
- **Volume/drive toolbar** above each pane: `\` (home), `..` (up), refresh
  (also re-scans for newly connected drives), then one button per
  filesystem root (drive letters on Windows, or `/` plus any mounted
  external volume on macOS/Linux) — scrollable, toggle via View menu/F9
  popup. Right-click a drive for Eject (macOS `diskutil`, Windows native
  IOCTLs — hidden for the boot volume, detected by device ID so a renamed
  "Macintosh HD" is still caught) or to open the native Disk
  Utility/Disk Management tool.
- **Browse into .zip archives** — double-click a .zip to browse it like a
  real directory (tabs, sorting, selection all work); F5 extracts instead of
  copying, F3 previews a file (or lets you pick one to preview without fully
  browsing in), and mutating operations refuse cleanly since archives are
  read-only.
- **Recursive search** (🔍 toolbar button, or Ctrl+F) by name or `*`/`?`
  wildcard pattern within the active tab's directory, with a selectable
  Depth limit (Unlimited / Just this folder / 1-10 levels deep) so an
  accidental search of somewhere huge doesn't run away; picking a match
  opens its location in a new tab, in the same view mode you were already
  using, with the file selected and scrolled into view.
- **Connections manager** (File menu / F9 popup, or the 🖥 button next to 🔍
  on each pane) — save named SFTP, SMB, or FileAgent connections and open one
  in a new tab that browses, renames, moves, creates folders in, and deletes
  from the remote server exactly like a local directory. Passwords/
  passphrases/pre-shared keys live in the OS keychain, never in the saved
  connection details on disk; SFTP host keys use standard SSH
  trust-on-first-use. FileAgent connects straight to a
  [KrankyBearFileMover](https://github.com/amarillier/KrankyBearFileMover)
  instance running as -file-agent on another machine, authenticating with a
  pre-shared key and a TLS certificate pin instead of a username. Host/Port
  aren't limited to the standard ports, so a firewall-forwarded port works
  too, the same as an ssh -p alias would. F4 Edit, Compress, Create Symbolic
  Link (SFTP/SMB), Multi-Rename Tool, Add to Favorites (reconnects
  automatically when clicked later), and Copy/Move between two different
  connections all work against a connection tab too, not just a local one.
- **Built-in viewer and editor** (F3/F4) — text or a hex dump for binary
  files, and a simple text editor with Save/Save As. F4 can also launch any
  number of **external editors** you configure (name + command); pick the
  default from the popup menu, per-file overrides included.
- **Favorites** — a shared bookmark list (with your filesystem's volumes
  listed alongside it) available from either pane; right-click any directory
  to bookmark it, or use "Add Current Directory…". Seeded on first run with
  common folders (Desktop, Downloads, and Applications on macOS).
- **Swap Panes** (Ctrl+U or the popup menu) — exchange the left and right
  panes' entire tab contents at once.
- **Switch Active Pane** (Ctrl+Tab, Ctrl+O, or the popup menu) — same as
  clicking into the other pane. Plain Tab isn't used for this: Fyne reserves
  it for cycling focus between controls before an app ever sees the
  keypress; Ctrl+O is there as a reliable alternative on platforms where
  Ctrl+Tab is reserved for something else (e.g. cycling a window's own
  native tabs on macOS).
- **Refresh** (F2, Ctrl+R, or either pane's own ⟳ drive-bar button) — always
  refreshes both panes and both drive bars at once, not just whichever one
  you triggered it from, so it's never ambiguous whether "nothing changed"
  really means nothing changed.
- **Show Hidden Files** toggle (View menu / F9 popup) — persists across
  launches.
- **Status bar** showing the cursor item's name, size/modified time (or item
  count for a directory), plus a live selection summary.
- **Customizable panel colors** — defaults to a Norton-Commander-style
  scheme (navy background, cyan/yellow/red text for normal/selected/cursor
  rows), fully customizable via a color picker; independent of the
  Light/Dark/System app theme.
- Double-click/Enter opens directories, launches other executables directly
  (detached — they keep running after you quit, and won't get wrapped in a
  Terminal window on macOS), and opens everything else with your OS's
  default application.
- Copy/Move run in the background with a progress dialog and
  Overwrite/Skip/Rename/Cancel conflict handling (with "apply to all").

## Cross-platform support

- **Linux**: GNOME, KDE, XFCE, Cinnamon, MATE, etc. on X11 or Wayland.
- **macOS**: 10.13 (High Sierra) or later.
- **Windows**: Windows 10 or later.

## Building & running

Requires Go and a Fyne-capable toolchain (CGo + OpenGL on desktop):

```
go run .
go build -o <app> .
```

Platform helpers: `compile-mac.sh`, `compile-win.sh`, `compile-linux.sh`, and
`package.sh` (`.deb`/`.rpm`, macOS `.pkg`).

## License

Free for personal, educational and commercial use, under the GNU GPL-3.0.

## Author

Allan Marillier

## Acknowledgments

- Built with [Fyne](https://fyne.io/) — an easy-to-use GUI toolkit for Go.
