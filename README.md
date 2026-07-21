# KrankyBear Commander

A cross-platform desktop application template built with Go and the
[Fyne](https://fyne.io/) GUI toolkit. Copy this template and run
`./rename-app.sh "New App Name" "Icon.png"` to start a new project.

> **PLACEHOLDER README** — `rename-app.sh` rewrites names, identifiers, icons and
> build/package scripts, but **not this prose**. Replace the description and the
> Features section below with your application's details (and likewise update
> `help.go`'s help text and `about.go`'s description). See `CLAUDE.md` for the
> per-project customization checklist.

Design philosophy aligns with Fyne: ease of use, solid functionality, steady bug
fixing and performance work.

## Features

- TODO: describe your application's features here.

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
