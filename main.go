package main

import (
	"flag"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	fynetooltip "github.com/dweymouth/fyne-tooltip"

	"commander/internal/panelstate"
)

const (
	// appName    = "KrankyBear Commander"
	appVersion = "0.2.0" // see FyneApp.toml
	appAuthor  = "Allan Marillier"
	appID      = "com.github.amarillier.KrankyBearCommander"
)

var appName = "KrankyBear Commander"
var appCopyright = buildCopyrightNotice()

func buildCopyrightNotice() string {
	const startYear = 2026
	currentYear := time.Now().Year()
	if currentYear <= startYear {
		return "Copyright (c) Allan Marillier, 2026"
	}
	return fmt.Sprintf("Copyright (c) Allan Marillier, 2026-%d", currentYear)
}

func main() {
	langFlag := flag.String("lang", "", "UI language code (e.g. en, de); overrides the saved preference for this run")
	flag.Parse()

	a := app.NewWithID(appID)
	a.SetIcon(resourceKrankyBearCommanderPng)
	setupI18n(a, *langFlag) // load message catalog + resolve UI language before building any UI
	loadTheme(a)

	win := a.NewWindow(appName)
	win.SetIcon(resourceKrankyBearCommanderPng)
	win.Resize(mainWindowLaunchSize(a)) // restore previous size (size only - Fyne can't restore position)

	cmdr = newCommander(a, win)
	// Wraps the window content in a tooltip render layer so the ttwidget
	// buttons used throughout (F-key bar, pane toolbar) can show their
	// SetToolTip text — torn down in quitApp.
	win.SetContent(fynetooltip.AddWindowToolTipLayer(cmdr.root, win.Canvas()))

	win.SetMainMenu(buildMenu(a, win))
	setupSystemTray(a, win)

	// Closing the window quits the app. Deferred via fyne.Do so quit() runs on a
	// clean loop iteration outside whatever callback triggered it — quitting
	// directly from inside a menu-item click or close-intercept callback can hang
	// on Windows (see CLAUDE.md "Quitting cleanly").
	win.SetCloseIntercept(func() { fyne.Do(func() { quitApp(a, win) }) })

	checkForUpdatesAuto(a) // quiet, once-per-day check; dialog only if an update exists

	win.ShowAndRun()
}

// ── Window geometry ──────────────────────────────────────────────────────────
// Fyne has no cross-platform window position/display restore, so only size is
// persisted (see CLAUDE.md "Window size persistence").

const (
	prefWinWidth  = "mainWindowWidth"
	prefWinHeight = "mainWindowHeight"
	minWinWidth   = 400
	minWinHeight  = 300
	maxWinDim     = 8000
	defaultWinW   = 1100
	defaultWinH   = 700
)

func mainWindowLaunchSize(a fyne.App) fyne.Size {
	w := a.Preferences().FloatWithFallback(prefWinWidth, defaultWinW)
	h := a.Preferences().FloatWithFallback(prefWinHeight, defaultWinH)
	if w < minWinWidth || w > maxWinDim {
		w = defaultWinW
	}
	if h < minWinHeight || h > maxWinDim {
		h = defaultWinH
	}
	return fyne.NewSize(float32(w), float32(h))
}

func saveMainWindowGeometry(a fyne.App, win fyne.Window) {
	size := win.Canvas().Size()
	a.Preferences().SetFloat(prefWinWidth, float64(size.Width))
	a.Preferences().SetFloat(prefWinHeight, float64(size.Height))
}

// quitApp does teardown in the order CLAUDE.md calls out: stop background work
// first (none yet in this bare template — add tickers/players above this call
// as the app grows), then persist geometry, then quit.
func quitApp(a fyne.App, win fyne.Window) {
	if cmdr != nil {
		cmdr.saveLayout()
	}
	saveMainWindowGeometry(a, win)
	fynetooltip.DestroyWindowToolTipLayer(win.Canvas())
	a.Quit()
}

// ── Menu + tray (mirror each other; see CLAUDE.md "System tray + main menu") ──

func buildMenu(a fyne.App, win fyne.Window) *fyne.MainMenu {
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Calculate Folder Sizes (active pane)", func() { cmdr.doCalculateFolderSizes() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { fyne.Do(func() { quitApp(a, win) }) }),
	)
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Brief View (active pane)", func() { cmdr.activePane().setViewMode(panelstate.ViewBrief) }),
		fyne.NewMenuItem("Full View (active pane)", func() { cmdr.activePane().setViewMode(panelstate.ViewExpanded) }),
		fyne.NewMenuItem("Panel Colors…", func() { showColorSchemeSettings(a, win, cmdr.applyColorScheme) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Light Theme", func() { setLightTheme(a) }),
		fyne.NewMenuItem("Dark Theme", func() { setDarkTheme(a) }),
		fyne.NewMenuItem("System Theme", func() { setSystemTheme(a) }),
	)
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Help", func() { showHelp(a) }),
		fyne.NewMenuItem("Check for Updates", func() { checkForUpdatesManual(a) }),
		fyne.NewMenuItem("About", func() { showAbout(a) }),
	)
	return fyne.NewMainMenu(fileMenu, viewMenu, helpMenu)
}

// setupSystemTray mirrors the main menu. Tray callbacks fire off the main
// goroutine, so every body is wrapped in fyne.Do (CLAUDE.md "fyne.Do is
// mandatory").
func setupSystemTray(a fyne.App, win fyne.Window) {
	desk, ok := a.(desktop.App)
	if !ok {
		return // not a desktop driver
	}
	menu := fyne.NewMenu(appName,
		fyne.NewMenuItem("Show", func() { fyne.Do(func() { win.Show(); win.RequestFocus() }) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Light Theme", func() { fyne.Do(func() { setLightTheme(a) }) }),
		fyne.NewMenuItem("Dark Theme", func() { fyne.Do(func() { setDarkTheme(a) }) }),
		fyne.NewMenuItem("System Theme", func() { fyne.Do(func() { setSystemTheme(a) }) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Help", func() { fyne.Do(func() { showHelp(a) }) }),
		fyne.NewMenuItem("Check for Updates", func() { checkForUpdatesManual(a) }),
		fyne.NewMenuItem("About", func() { fyne.Do(func() { showAbout(a) }) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { fyne.Do(func() { quitApp(a, win) }) }),
	)
	desk.SetSystemTrayMenu(menu)
	desk.SetSystemTrayIcon(resourceKrankyBearCommanderPng)
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
