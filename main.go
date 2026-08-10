package main

import (
	"flag"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"

	fynetooltip "github.com/dweymouth/fyne-tooltip"

	"commander/internal/panelstate"
	"commander/internal/startup"
)

const (
	// appName    = "KrankyBear Commander"
	appVersion = "1.3.1" // see FyneApp.toml
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
	mesaFallbackFlag := flag.Bool(startup.MesaFallbackFlagName, false, "internal: relaunch flag for the Mesa3D OpenGL fallback (Windows only)")
	flag.Parse()

	// Windows only (no-op elsewhere): prefer real hardware OpenGL, falling
	// back to the bundled Mesa3D software renderer (relaunching once) only
	// if the hardware probe fails. Must run before app.NewWithID — GLFW
	// only supports one Init/Terminate cycle per process, shared with
	// Fyne's own driver (see internal/startup/opengl.go).
	startup.EnsureWindowsOpenGLReady(*mesaFallbackFlag)

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

	// Drag files in from Finder/Explorer/Nautilus to copy them into
	// whichever pane they're dropped on (see dragdrop_ui.go).
	win.SetOnDropped(cmdr.handleDropped)

	// Closing the window quits the app. Deferred via fyne.Do so quit() runs on a
	// clean loop iteration outside whatever callback triggered it — quitting
	// directly from inside a menu-item click or close-intercept callback can hang
	// on Windows (see CLAUDE.md "Quitting cleanly").
	win.SetCloseIntercept(func() { fyne.Do(func() { quitApp(a, win) }) })

	checkForUpdatesAuto(a) // quiet, once-per-day check; dialog only if an update exists

	// Show() before installing drag-out: Fyne's GLFW driver creates the
	// real native window handle lazily on Show(), and dragout.Install
	// needs that handle to exist (see dragout_ui.go).
	win.Show()
	cmdr.installDragOut()
	a.Run()
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
// as the app grows), then persist geometry, then quit. If any Copy/Move
// operations are still running in the background (backgroundops_ui.go),
// confirms first rather than silently killing them mid-transfer.
func quitApp(a fyne.App, win fyne.Window) {
	if cmdr != nil {
		if n := len(cmdr.backgroundOps); n > 0 {
			showDialog(dialog.NewConfirm("Quit KrankyBear Commander",
				fmt.Sprintf("%d background operation(s) are still running. Quit anyway?", n),
				func(ok bool) {
					if ok {
						doQuit(a, win)
					}
				}, win))
			return
		}
	}
	doQuit(a, win)
}

func doQuit(a fyne.App, win fyne.Window) {
	if cmdr != nil {
		cmdr.saveLayout()
	}
	saveMainWindowGeometry(a, win)
	fynetooltip.DestroyWindowToolTipLayer(win.Canvas())
	a.Quit()
}

// ── Menu + tray (mirror each other; see CLAUDE.md "System tray + main menu") ──

func buildMenu(a fyne.App, win fyne.Window) *fyne.MainMenu {
	editorsItem := fyne.NewMenuItem("Manage Editors…", func() { cmdr.showManageEditors() })
	connectionsItem := fyne.NewMenuItem("Connections…", func() { cmdr.showConnections(cmdr.activePane()) })
	launcherItem := fyne.NewMenuItem("Application Launcher…", func() { cmdr.showLauncherMenu(cmdr.activePane()) })
	backgroundOpsLabel := "Background Operations…"
	if n := len(cmdr.backgroundOps); n > 0 {
		backgroundOpsLabel = fmt.Sprintf("Background Operations (%d)…", n)
	}
	backgroundOpsItem := fyne.NewMenuItem(backgroundOpsLabel, func() { cmdr.showBackgroundOperations() })
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Calculate Folder Sizes (active pane)", func() { cmdr.doCalculateFolderSizes() }),
		fyne.NewMenuItem("Search… (active pane) (Ctrl+F)", func() { cmdr.showSearch(cmdr.activePane()) }),
		fyne.NewMenuItem("Compare/Synchronize Directories…", func() { cmdr.showCompareSync(comparePrimaryNone) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Copy (Ctrl/Cmd+C)", func() { cmdr.doCopyToClipboard() }),
		fyne.NewMenuItem("Paste (Ctrl/Cmd+V)", func() { cmdr.doPaste() }),
		backgroundOpsItem,
		fyne.NewMenuItemSeparator(),
		editorsItem,
		connectionsItem,
		launcherItem,
		fyne.NewMenuItem("7-Zip Binary Path…", func() { cmdr.showSevenZipSettings() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Export Settings…", func() { cmdr.showExportSettings() }),
		fyne.NewMenuItem("Import Settings…", func() { cmdr.showImportSettings() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { fyne.Do(func() { quitApp(a, win) }) }),
	)
	hiddenFilesItem := fyne.NewMenuItem("Show Hidden Files", func() {
		cmdr.toggleHiddenFiles()
		fyne.Do(func() { win.SetMainMenu(buildMenu(a, win)) })
	})
	hiddenFilesItem.Checked = cmdr.showHiddenFiles
	driveBarItem := fyne.NewMenuItem("Show Volume/Drive Toolbar", func() {
		cmdr.toggleDriveBar()
		fyne.Do(func() { win.SetMainMenu(buildMenu(a, win)) })
	})
	driveBarItem.Checked = cmdr.showDriveBar
	cmdLineItem := fyne.NewMenuItem("Show Command Line", func() {
		cmdr.toggleShowCmdLine()
		fyne.Do(func() { win.SetMainMenu(buildMenu(a, win)) })
	})
	cmdLineItem.Checked = cmdr.showCmdLine
	briefColumnsItem := fyne.NewMenuItem("Brief Columns", nil)
	briefColumnsItem.ChildMenu = cmdr.buildBriefColumnsSubmenu(func() { fyne.Do(func() { win.SetMainMenu(buildMenu(a, win)) }) })
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Brief View (active pane)", func() { cmdr.activePane().setViewMode(panelstate.ViewBrief) }),
		fyne.NewMenuItem("Full View (active pane)", func() { cmdr.activePane().setViewMode(panelstate.ViewExpanded) }),
		briefColumnsItem,
		fyne.NewMenuItem("Refresh Both Panes (F2 / Ctrl+R)", func() { cmdr.doRefresh() }),
		fyne.NewMenuItem("Switch Active Pane (Ctrl+Tab / Ctrl+O)", func() { cmdr.toggleActivePane() }),
		fyne.NewMenuItem("Swap Panes (Ctrl+U)", func() { cmdr.swapPanes() }),
		hiddenFilesItem,
		driveBarItem,
		cmdLineItem,
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
	// Unlike the main menu (rebuilt on every background-op start/stop via
	// commander.refreshMainMenu, so its label can show a live count), the
	// tray menu is built once at startup and never rebuilt for anything
	// else in this app — so this item stays a plain, static label; opening
	// it still shows live counts/rows, the label itself just doesn't.
	menu := fyne.NewMenu(appName,
		fyne.NewMenuItem("Show", func() { fyne.Do(func() { win.Show(); win.RequestFocus() }) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Background Operations…", func() { fyne.Do(func() { cmdr.showBackgroundOperations() }) }),
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
