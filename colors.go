// colors.go — the pane color scheme (background/normal/selected/cursor text).
// This is independent of the Light/Dark/System app-chrome theme in theme.go:
// the user asked specifically for Norton-Commander-style pane colors (dark
// navy background, cyan normal text, yellow selected text, red active-cursor
// text) that they can tweak, regardless of which overall app theme is active.
// No font customization is offered — colors are the only tunable here.
package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ColorScheme is the pane listing's 4 user-configurable colors.
type ColorScheme struct {
	PanelBG      color.Color
	TextNormal   color.Color
	TextSelected color.Color
	TextCursor   color.Color
}

// classicBlueScheme returns the Norton-Commander-style defaults: dark navy
// panel background, cyan normal text, yellow selected text, red cursor text.
func classicBlueScheme() ColorScheme {
	return ColorScheme{
		PanelBG:      color.NRGBA{R: 0x00, G: 0x00, B: 0x80, A: 0xff},
		TextNormal:   color.NRGBA{R: 0x00, G: 0xff, B: 0xff, A: 0xff},
		TextSelected: color.NRGBA{R: 0xff, G: 0xff, B: 0x00, A: 0xff},
		TextCursor:   color.NRGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff},
	}
}

const (
	prefColorPanelBG      = "colorPanelBG"
	prefColorTextNormal   = "colorTextNormal"
	prefColorTextSelected = "colorTextSelected"
	prefColorTextCursor   = "colorTextCursor"
)

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

// showColorSchemeSettings opens a dialog with one color picker swatch per
// scheme color plus a "Reset to Classic Blue" button. onChange is called
// (with the scheme so far) after every individual color pick, so open panes
// repaint live rather than only after the whole dialog is confirmed.
func showColorSchemeSettings(a fyne.App, win fyne.Window, onChange func(ColorScheme)) {
	cs := loadColorScheme(a)

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
			picker.Show()
		}
		return btn
	}

	rows := container.NewVBox(
		swatch("Panel Background…", func(c ColorScheme) color.Color { return c.PanelBG }, func(c *ColorScheme, v color.Color) { c.PanelBG = v }),
		swatch("Normal Text…", func(c ColorScheme) color.Color { return c.TextNormal }, func(c *ColorScheme, v color.Color) { c.TextNormal = v }),
		swatch("Selected Text…", func(c ColorScheme) color.Color { return c.TextSelected }, func(c *ColorScheme, v color.Color) { c.TextSelected = v }),
		swatch("Active Cursor Text…", func(c ColorScheme) color.Color { return c.TextCursor }, func(c *ColorScheme, v color.Color) { c.TextCursor = v }),
	)

	resetBtn := widget.NewButton("Reset to Classic Blue", func() {
		cs = classicBlueScheme()
		saveColorScheme(a, cs)
		onChange(cs)
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Panel Colors", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		rows,
		widget.NewSeparator(),
		resetBtn,
	)

	d := dialog.NewCustom("Panel Colors", "Close", container.NewPadded(content), win)
	d.Show()
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
