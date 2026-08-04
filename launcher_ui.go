// launcher_ui.go — the Application Launcher: a per-pane toolbar button
// opening a filterable, alphabetized list of user-added applications;
// clicking one launches it detached (internal/launch.Run — survives
// Commander closing, never an orphan/zombie child of it). internal/
// launchers owns persistence; this file is the Fyne-facing half, deliberately
// simpler than TotalCmd's icon toolbars — a plain text list with a filter,
// no icons, no drag-and-drop-positioned buttons, matching this app's own
// "simple and functional" convention. Each entry can optionally set
// Parameters/Start In/Environment Variables (see showLauncherForm) — ideas
// borrowed from ../KrankyBearExecutor, minus its Comments/alias and
// multi-app Groups, which aren't wanted here.
package main

import (
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"commander/internal/launch"
	"commander/internal/launchers"
)

func (c *commander) launchersPath() string {
	p, err := launchers.DefaultPath(appName)
	if err != nil {
		return ""
	}
	return p
}

func (c *commander) loadLaunchers() {
	path := c.launchersPath()
	if path == "" {
		return
	}
	if cfg, err := launchers.Load(path); err == nil {
		c.launcherConfig = cfg
	}
}

func (c *commander) saveLaunchers() {
	path := c.launchersPath()
	if path == "" {
		return
	}
	_ = launchers.Save(path, c.launcherConfig)
}

// launcherNameFromPath derives a default launcher name from an added
// file's path — e.g. "/Applications/Visual Studio Code.app" -> "Visual
// Studio Code", "C:\Tools\rapidee.exe" -> "rapidee".
func launcherNameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// sortedLauncherNames returns every configured launcher's name, matching
// filter case-insensitively (a blank filter matches everything), sorted
// alphabetically.
func sortedLauncherNames(cfg launchers.Config, filter string) []string {
	filter = strings.ToLower(filter)
	names := make([]string, 0, len(cfg.Launchers))
	for _, l := range cfg.Launchers {
		if filter == "" || strings.Contains(strings.ToLower(l.Name), filter) {
			names = append(names, l.Name)
		}
	}
	sort.Strings(names)
	return names
}

// parseEnvLines splits a "KEY=VALUE" per line Environment Variables field
// into the []string os/exec itself expects, skipping blank lines. Lines
// without an "=" are passed through as-is (exec/the OS will simply reject
// a malformed entry, same as a typo in a real KEY=VALUE pair would).
func parseEnvLines(s string) []string {
	var env []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			env = append(env, line)
		}
	}
	return env
}

// runLauncher launches l per its own Parameters/Start In/Environment
// Variables, parsed at launch time (see internal/launch.SplitArgs,
// parseEnvLines) — never at save time, matching how every other free-text
// field in this app works.
func runLauncher(l launchers.Launcher) error {
	return launch.Run(l.Command, launch.RunOptions{
		Args:       launch.SplitArgs(l.Args),
		WorkingDir: l.WorkingDir,
		Env:        parseEnvLines(l.Env),
	})
}

// showLauncherMenu opens the Application Launcher dialog for pane p — p is
// only used to decide where launched-app errors surface (c.win, actually;
// p is unused today but kept for symmetry with showConnections/
// showFavoritesMenu's own per-pane signature, in case a future launcher
// feature becomes pane-specific).
func (c *commander) showLauncherMenu(p *pane) {
	list := container.NewVBox()
	filterEntry := newDialogEntry()
	filterEntry.SetPlaceHolder("Filter…")

	var d dialog.Dialog
	var refresh func()

	launchAndClose := func(name string) {
		l, ok := c.launcherConfig.Find(name)
		if !ok {
			return
		}
		if err := runLauncher(l); err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		d.Hide()
	}

	refresh = func() {
		names := sortedLauncherNames(c.launcherConfig, filterEntry.Text)
		var rows []fyne.CanvasObject
		if len(c.launcherConfig.Launchers) == 0 {
			rows = append(rows, widget.NewLabel("No applications added yet — drag one in, or use Add… below."))
		} else if len(names) == 0 {
			rows = append(rows, widget.NewLabel("No matches."))
		}
		for _, name := range names {
			l, _ := c.launcherConfig.Find(name)
			label := newTappableLabel(name, func() { launchAndClose(name) })
			editBtn := widget.NewButton("Edit…", func() {
				c.showLauncherForm(&l, refresh)
			})
			removeBtn := widget.NewButton("×", func() {
				c.launcherConfig.Remove(name)
				c.saveLaunchers()
				refresh()
			})
			removeBtn.Importance = widget.LowImportance
			rows = append(rows, container.NewBorder(nil, nil, nil, container.NewHBox(editBtn, removeBtn), label))
		}
		list.Objects = rows
		list.Refresh()
	}
	filterEntry.OnChanged = func(string) { refresh() }
	refresh()

	addBtn := widget.NewButton("Add…", func() { c.showLauncherForm(nil, refresh) })

	content := container.NewBorder(
		filterEntry, addBtn, nil, nil,
		container.NewVScroll(list),
	)

	d = dialog.NewCustom("Application Launcher", "Close", content, c.win)
	d.Resize(fyne.NewSize(460, 480))
	// Drag-and-drop quick-add (dragdrop_ui.go's handleDropped, while this
	// popup is open) — Name+Command only, same fast path as before, now
	// also defaulting Start In to the dropped file's own directory for a
	// NEW entry (an existing same-name entry's own WorkingDir, if any, is
	// left alone — only its Command is refreshed, matching Config.Add's
	// existing "replace by name" behavior).
	c.launcherPopupAdd = func(name, command string) {
		if _, exists := c.launcherConfig.Find(name); !exists {
			c.launcherConfig.Upsert(launchers.Launcher{Name: name, Command: command, WorkingDir: filepath.Dir(command)})
		} else {
			c.launcherConfig.Add(name, command)
		}
		c.saveLaunchers()
		refresh()
	}
	d.SetOnClosed(func() { c.launcherPopupAdd = nil })
	showDialog(d)
	c.win.Canvas().Focus(filterEntry)
}

// showLauncherForm is Add (existing == nil) or Edit (existing != nil) — the
// full form for a launcher's optional Parameters/Start In/Environment
// Variables, on top of showLauncherMenu's own list (mirrors connections_ui.go's
// showConnectionForm: its own dialog, Save persists and closes just this
// one, then calls onSaved so the list refreshes).
func (c *commander) showLauncherForm(existing *launchers.Launcher, onSaved func()) {
	nameEntry := newDialogEntry()
	nameEntry.SetPlaceHolder("Name")
	commandEntry := newDialogEntry()
	commandEntry.SetPlaceHolder("Command (the application to launch)")
	argsEntry := newDialogEntry()
	argsEntry.SetPlaceHolder(`Parameters (e.g. --title "My App")`)
	workDirEntry := newDialogEntry()
	workDirEntry.SetPlaceHolder("Start In (defaults to the command's own directory)")
	envEntry := newDialogEntry()
	envEntry.MultiLine = true
	envEntry.Wrapping = fyne.TextWrapOff
	envEntry.SetPlaceHolder("Environment Variables — one KEY=VALUE per line")

	browseFor := func(entry *dialogEntry, defaultToDir bool) *widget.Button {
		return widget.NewButton("Browse…", func() {
			fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
				if err != nil || uc == nil {
					return
				}
				defer uc.Close()
				path := uc.URI().Path()
				entry.SetText(path)
				if defaultToDir && nameEntry.Text == "" {
					nameEntry.SetText(launcherNameFromPath(path))
				}
				if defaultToDir && workDirEntry.Text == "" {
					workDirEntry.SetText(filepath.Dir(path))
				}
			}, c.win)
			if home, err := c.fs.HomeDir(); err == nil && home != "" {
				if uri := storage.NewFileURI(home); uri != nil {
					if lister, err := storage.ListerForURI(uri); err == nil {
						fd.SetLocation(lister)
					}
				}
			}
			showDialog(fd)
		})
	}

	title := "Add Application"
	originalName := ""
	if existing != nil {
		title = "Edit Application"
		originalName = existing.Name
		nameEntry.SetText(existing.Name)
		commandEntry.SetText(existing.Command)
		argsEntry.SetText(existing.Args)
		workDirEntry.SetText(existing.WorkingDir)
		envEntry.SetText(existing.Env)
	}

	content := container.NewVBox(
		container.NewGridWithColumns(2, widget.NewLabel("Name:"), nameEntry),
		container.NewGridWithColumns(2, widget.NewLabel("Command:"), container.NewBorder(nil, nil, nil, browseFor(commandEntry, true), commandEntry)),
		container.NewGridWithColumns(2, widget.NewLabel("Parameters:"), argsEntry),
		container.NewGridWithColumns(2, widget.NewLabel("Start In:"), container.NewBorder(nil, nil, nil, browseFor(workDirEntry, false), workDirEntry)),
		widget.NewLabel("Environment Variables:"),
		envEntry,
	)

	d := dialog.NewCustomConfirm(title, "Save", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(nameEntry.Text)
		command := strings.TrimSpace(commandEntry.Text)
		if name == "" || command == "" {
			return
		}
		if originalName != "" && originalName != name {
			c.launcherConfig.Remove(originalName)
		}
		c.launcherConfig.Upsert(launchers.Launcher{
			Name:       name,
			Command:    command,
			Args:       strings.TrimSpace(argsEntry.Text),
			WorkingDir: strings.TrimSpace(workDirEntry.Text),
			Env:        strings.TrimSpace(envEntry.Text),
		})
		c.saveLaunchers()
		if onSaved != nil {
			onSaved()
		}
	}, c.win)
	d.Resize(fyne.NewSize(520, 420))
	showDialog(d)
	c.win.Canvas().Focus(nameEntry)
}
