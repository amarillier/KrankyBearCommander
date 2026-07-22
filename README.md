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
  snap back to the locked root).
- **Brief and Full (detailed) views** — Brief is a compact, name-only wrapped
  grid; Full adds sortable Name / Ext / Size / Modified / Permissions
  columns (click a header to sort, click again to reverse).
- **Classic F-key row**: F1 Help, F3 View, F4 Edit, F5 Copy, F6 Move/Rename,
  F7 MkDir, F8 Delete (to trash), Shift+F8 delete permanently, F9 menu, F10
  Quit — both as on-screen buttons and real keyboard shortcuts, with tooltips
  on every button.
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
