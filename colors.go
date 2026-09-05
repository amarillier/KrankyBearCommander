// colors.go — the pane color scheme (background/normal/selected/cursor text)
// and named color THEMES built on top of it. This is independent of the
// Light/Dark/System app-chrome theme in theme.go: the user asked
// specifically for Norton-Commander-style pane colors (dark navy background,
// cyan normal text, yellow selected text, red active-cursor text) that they
// can tweak, regardless of which overall app theme is active. No font
// customization is offered — colors are the only tunable here.
//
// A theme (internal/themes.Theme) is just this same 5-color scheme plus a
// Name, round-tripped as JSON (hex strings, not image/color.Color, hence the
// themeToColorScheme/colorSchemeToTheme conversions below). Two sources feed
// the picker in showColorSchemeSettings: the starter themes bundled with the
// app (themes_bundled.go, assets/themes/*.json, read-only) and the user's
// own saved/imported themes (internal/themes.UserDir, a real per-user
// directory — NOT assets/, which is this app's own bundled, git-tracked
// resource tree, not something a running install can usefully write a
// user's custom theme into).
package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"commander/internal/themes"
)

// ColorScheme is the pane listing's 5 user-configurable colors.
type ColorScheme struct {
	PanelBG      color.Color
	TextNormal   color.Color
	TextSelected color.Color
	TextCursor   color.Color
	TextDir      color.Color // directories and ".." — normal-priority, below selected/cursor
}

// classicBlueScheme returns the Norton-Commander-style defaults: dark navy
// panel background, cyan normal text, yellow selected text, red cursor text,
// white directory text.
func classicBlueScheme() ColorScheme {
	return ColorScheme{
		PanelBG:      color.NRGBA{R: 0x00, G: 0x00, B: 0x80, A: 0xff},
		TextNormal:   color.NRGBA{R: 0x00, G: 0xff, B: 0xff, A: 0xff},
		TextSelected: color.NRGBA{R: 0xff, G: 0xff, B: 0x00, A: 0xff},
		TextCursor:   color.NRGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff},
		TextDir:      color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}
}

const (
	prefColorPanelBG      = "colorPanelBG"
	prefColorTextNormal   = "colorTextNormal"
	prefColorTextSelected = "colorTextSelected"
	prefColorTextCursor   = "colorTextCursor"
	prefColorTextDir      = "colorTextDir"
	// prefColorThemeName remembers which named theme (if any) the current
	// colors came from, purely so the picker in showColorSchemeSettings can
	// re-select it next time the dialog opens. It's cosmetic, and easily
	// stale: further swatch tweaks after picking a theme don't clear it, so
	// it can end up naming a theme the current colors no longer exactly
	// match — that's fine, the actual colors always come from
	// prefColor{PanelBG,...} above, never reconstructed from this name.
	prefColorThemeName = "colorThemeName"
)

// themeToColorScheme converts a saved/bundled theme's hex strings into a
// ColorScheme, falling back per-field to classicBlueScheme() for anything
// missing or malformed (e.g. a hand-edited theme file) — same tolerance
// loadColorScheme already gives Preferences-stored colors.
func themeToColorScheme(t themes.Theme) ColorScheme {
	def := classicBlueScheme()
	return ColorScheme{
		PanelBG:      hexOrDefault(t.PanelBG, def.PanelBG),
		TextNormal:   hexOrDefault(t.TextNormal, def.TextNormal),
		TextSelected: hexOrDefault(t.TextSelected, def.TextSelected),
		TextCursor:   hexOrDefault(t.TextCursor, def.TextCursor),
		TextDir:      hexOrDefault(t.TextDir, def.TextDir),
	}
}

// colorSchemeToTheme is themeToColorScheme's inverse, for Save As/Export.
func colorSchemeToTheme(name string, cs ColorScheme) themes.Theme {
	return themes.Theme{
		Name:         name,
		PanelBG:      colorToHex(cs.PanelBG),
		TextNormal:   colorToHex(cs.TextNormal),
		TextSelected: colorToHex(cs.TextSelected),
		TextCursor:   colorToHex(cs.TextCursor),
		TextDir:      colorToHex(cs.TextDir),
	}
}

// userThemesDir resolves internal/themes.UserDir, logging nothing and
// simply disabling Save As/user-theme-listing (via an empty path, which
// themes.LoadAllUser/Save tolerate) if the OS somehow has no config dir.
func userThemesDir() string {
	dir, err := themes.UserDir(appName)
	if err != nil {
		return ""
	}
	return dir
}

// loadColorScheme reads the persisted scheme, falling back to classicBlueScheme
// for any color that was never saved (first launch).
func loadColorScheme(a fyne.App) ColorScheme {
	def := classicBlueScheme()
	prefs := a.Preferences()
	return ColorScheme{
		PanelBG:      hexOrDefault(prefs.String(prefColorPanelBG), def.PanelBG),
		TextNormal:   hexOrDefault(prefs.String(prefColorTextNormal), def.TextNormal),
		TextSelected: hexOrDefault(prefs.String(prefColorTextSelected), def.TextSelected),
		TextCursor:   hexOrDefault(prefs.String(prefColorTextCursor), def.TextCursor),
		TextDir:      hexOrDefault(prefs.String(prefColorTextDir), def.TextDir),
	}
}

// saveColorScheme persists cs as hex strings via Preferences (same mechanism
// theme.go already uses for the light/dark/system choice).
func saveColorScheme(a fyne.App, cs ColorScheme) {
	prefs := a.Preferences()
	prefs.SetString(prefColorPanelBG, colorToHex(cs.PanelBG))
	prefs.SetString(prefColorTextNormal, colorToHex(cs.TextNormal))
	prefs.SetString(prefColorTextSelected, colorToHex(cs.TextSelected))
	prefs.SetString(prefColorTextCursor, colorToHex(cs.TextCursor))
	prefs.SetString(prefColorTextDir, colorToHex(cs.TextDir))
}

func colorToHex(c color.Color) string {
	nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", nrgba.R, nrgba.G, nrgba.B)
}

// hexOrDefault parses "#rrggbb"; an empty or malformed string returns def.
func hexOrDefault(hex string, def color.Color) color.Color {
	if len(hex) != 7 || hex[0] != '#' {
		return def
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return def
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}

// showColorSchemeSettings opens a dialog with a Theme picker (bundled
// starter themes plus the user's own saved/imported ones), one color picker
// swatch per scheme color for further tweaking after picking a theme, and
// Save As/Import/Export for managing custom theme files. onChange is called
// (with the scheme so far) after every individual color change — a theme
// pick, a swatch edit, or an import — so open panes repaint live rather
// than only after the whole dialog is confirmed.
func showColorSchemeSettings(a fyne.App, win fyne.Window, onChange func(ColorScheme)) {
	cs := loadColorScheme(a)
	userDir := userThemesDir()

	swatch := func(label string, get func(ColorScheme) color.Color, set func(*ColorScheme, color.Color)) fyne.CanvasObject {
		btn := widget.NewButton(label, nil)
		btn.OnTapped = func() {
			picker := dialog.NewColorPicker(label, "Choose a color", func(c color.Color) {
				if c == nil {
					return
				}
				set(&cs, c)
				saveColorScheme(a, cs)
				onChange(cs)
			}, win)
			picker.Advanced = true
			picker.SetColor(get(cs))
			showDialog(picker)
		}
		return btn
	}

	rows := container.NewVBox(
		swatch("Panel Background…", func(c ColorScheme) color.Color { return c.PanelBG }, func(c *ColorScheme, v color.Color) { c.PanelBG = v }),
		swatch("Normal Text…", func(c ColorScheme) color.Color { return c.TextNormal }, func(c *ColorScheme, v color.Color) { c.TextNormal = v }),
		swatch("Selected Text…", func(c ColorScheme) color.Color { return c.TextSelected }, func(c *ColorScheme, v color.Color) { c.TextSelected = v }),
		swatch("Active Cursor Text…", func(c ColorScheme) color.Color { return c.TextCursor }, func(c *ColorScheme, v color.Color) { c.TextCursor = v }),
		swatch("Directory Text…", func(c ColorScheme) color.Color { return c.TextDir }, func(c *ColorScheme, v color.Color) { c.TextDir = v }),
	)

	// byName backs the picker's OnChanged lookup — rebuilt every time the
	// available theme set changes (Save As/Import add to it) via
	// rebuildPickerOptions below.
	byName := map[string]themes.Theme{}
	var picker *widget.Select
	rebuildPickerOptions := func(selectName string) {
		list := append([]themes.Theme{}, bundledThemes()...)
		if userDir != "" {
			list = append(list, themes.LoadAllUser(userDir)...)
		}
		names := make([]string, 0, len(list))
		byName = make(map[string]themes.Theme, len(list))
		for _, t := range list {
			names = append(names, t.Name)
			byName[t.Name] = t
		}
		picker.Options = names
		picker.Refresh()
		if _, ok := byName[selectName]; ok {
			picker.SetSelected(selectName)
		} else {
			picker.ClearSelected()
		}
	}
	picker = widget.NewSelect(nil, func(name string) {
		t, ok := byName[name]
		if !ok {
			return
		}
		cs = themeToColorScheme(t)
		saveColorScheme(a, cs)
		a.Preferences().SetString(prefColorThemeName, t.Name)
		onChange(cs)
	})
	rebuildPickerOptions(a.Preferences().StringWithFallback(prefColorThemeName, ""))

	saveAsBtn := widget.NewButton("Save As New Theme…", func() {
		if userDir == "" {
			dialog.ShowError(fmt.Errorf("no per-user settings directory available on this system"), win)
			return
		}
		nameEntry := newDialogEntry()
		nameEntry.SetPlaceHolder("Theme name")
		if cur := a.Preferences().StringWithFallback(prefColorThemeName, ""); cur != "" {
			nameEntry.SetText(cur)
			nameEntry.CursorColumn = len([]rune(cur))
		}
		d := dialog.NewCustomConfirm("Save Theme", "Save", "Cancel", nameEntry, func(ok bool) {
			name := strings.TrimSpace(nameEntry.Text)
			if !ok || name == "" {
				return
			}
			if _, err := themes.Save(userDir, colorSchemeToTheme(name, cs)); err != nil {
				dialog.ShowError(err, win)
				return
			}
			a.Preferences().SetString(prefColorThemeName, name)
			rebuildPickerOptions(name)
		}, win)
		showDialog(d)
		win.Canvas().Focus(nameEntry)
		nameEntry.TypedShortcut(&fyne.ShortcutSelectAll{})
	})

	importBtn := widget.NewButton("Import Theme…", func() {
		fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
			if err != nil || uc == nil {
				return
			}
			defer uc.Close()
			data, err := io.ReadAll(uc)
			if err != nil {
				dialog.ShowError(err, win)
				return
			}
			var t themes.Theme
			if err := json.Unmarshal(data, &t); err != nil || strings.TrimSpace(t.Name) == "" {
				dialog.ShowError(fmt.Errorf("not a valid theme file"), win)
				return
			}
			cs = themeToColorScheme(t)
			saveColorScheme(a, cs)
			a.Preferences().SetString(prefColorThemeName, t.Name)
			onChange(cs)
			// Best-effort: also copy it into the user's own themes directory
			// so it's still in the picker on the next launch, not just this
			// session — a failure here (e.g. read-only config dir) doesn't
			// stop the import from taking effect right now.
			if userDir != "" {
				themes.Save(userDir, t)
			}
			rebuildPickerOptions(t.Name)
		}, win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
		showDialog(fd)
	})

	exportBtn := widget.NewButton("Export Theme…", func() {
		name := a.Preferences().StringWithFallback(prefColorThemeName, "Custom Theme")
		t := colorSchemeToTheme(name, cs)
		fd := dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
			if err != nil || uc == nil {
				return
			}
			defer uc.Close()
			b, err := json.MarshalIndent(t, "", "  ")
			if err != nil {
				dialog.ShowError(err, win)
				return
			}
			if _, err := uc.Write(b); err != nil {
				dialog.ShowError(err, win)
			}
		}, win)
		fd.SetFileName(strings.ReplaceAll(t.Name, "/", "-") + ".json")
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
		showDialog(fd)
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Panel Colors", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewLabel("Theme:"),
		picker,
		widget.NewSeparator(),
		rows,
		widget.NewSeparator(),
		container.NewGridWithColumns(3, saveAsBtn, importBtn, exportBtn),
	)

	d := dialog.NewCustom("Panel Colors", "Close", container.NewPadded(content), win)
	showDialog(d)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
