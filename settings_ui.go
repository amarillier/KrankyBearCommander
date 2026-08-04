// settings_ui.go — Export/Import Settings (File menu / F9 popup): bundles
// Fyne's own preferences.json (window size, panel colors, hidden-files/
// drive-bar toggles, column widths, 7-Zip path, Multi-Rename's remembered
// pattern, ...) together with this app's own JSON configs (Favorites,
// Editors, Connections, Application Launcher, window/tab layout) into one
// zip, so moving settings to another machine (or just backing them up) is
// a single file instead of hunting through several OS-specific locations.
// The update-checker's cache (latestcheck-*.json) is deliberately excluded
// — it's cached state, not a real setting, and importing a stale "last
// checked" timestamp on another machine could even suppress a legitimate
// check there.
//
// Import writes files straight to disk and then prompts to quit rather
// than trying to take effect live: Fyne keeps an in-memory copy of every
// preference for the app's whole lifetime, and unconditionally rewrites
// preferences.json from THAT on every quit — an import that lands while
// Commander is still running would just get overwritten by the next save,
// including the guaranteed one on shutdown. A restart is the only
// reliable way to guarantee the imported values actually take hold.
package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"commander/internal/fsops"
)

// settingsFileNames are every file Export/Import Settings moves as one
// unit — "preferences.json" lives under c.app.Storage().RootURI() (Fyne's
// own storage root, wherever the current OS puts it); the rest live
// alongside each other under os.UserConfigDir()/appName.
var settingsFileNames = []string{
	"preferences.json",
	"favorites.json",
	"editors.json",
	"connections.json",
	"launchers.json",
	"layout.json",
}

// settingsFilePaths resolves every name in settingsFileNames to its real
// on-disk path — using c.app.Storage().RootURI() for Fyne's own
// preferences.json rather than hardcoding any OS-specific location.
func (c *commander) settingsFilePaths() (map[string]string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	configDir = filepath.Join(configDir, appName)

	paths := map[string]string{
		"preferences.json": filepath.Join(c.app.Storage().RootURI().Path(), "preferences.json"),
	}
	for _, name := range settingsFileNames {
		if name == "preferences.json" {
			continue
		}
		paths[name] = filepath.Join(configDir, name)
	}
	return paths, nil
}

// showExportSettings prompts for where to save a zip containing every
// settings file that actually exists yet (a fresh install won't have
// Connections/Launchers/etc. until first used).
func (c *commander) showExportSettings() {
	paths, err := c.settingsFilePaths()
	if err != nil {
		dialog.ShowError(err, c.win)
		return
	}
	var sources []string
	for _, name := range settingsFileNames {
		if p, ok := paths[name]; ok {
			if _, err := os.Stat(p); err == nil {
				sources = append(sources, p)
			}
		}
	}
	if len(sources) == 0 {
		c.showStatus("nothing to export yet")
		return
	}

	fd := dialog.NewFileSave(func(uc fyne.URIWriteCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		defer uc.Close()
		tmp, err := os.CreateTemp("", "krankybear-settings-*.zip")
		if err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)

		if err := fsops.Compress(sources, tmpPath); err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		in, err := os.Open(tmpPath)
		if err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		defer in.Close()
		if _, err := io.Copy(uc, in); err != nil {
			dialog.ShowError(err, c.win)
		}
	}, c.win)
	fd.SetFileName(appName + "-settings.zip")
	showDialog(fd)
}

// showImportSettings picks a settings zip, confirms (with a clear note
// that keychain-stored secrets aren't included), applies it, then prompts
// to quit so the imported values actually take effect (see this file's
// top doc comment for why a live reload isn't reliable).
func (c *commander) showImportSettings() {
	fd := dialog.NewFileOpen(func(uc fyne.URIReadCloser, err error) {
		if err != nil || uc == nil {
			return
		}
		defer uc.Close()
		data, err := io.ReadAll(uc)
		if err != nil {
			dialog.ShowError(err, c.win)
			return
		}
		showDialog(dialog.NewConfirm("Import Settings",
			"This replaces your current Favorites, Editors, Connections, Application "+
				"Launcher list, window/tab layout, and general preferences with the ones "+
				"in this file.\n\nSaved connection/launcher passwords, SSH key "+
				"passphrases, and FileAgent pre-shared keys live only in the OS keychain "+
				"and aren't included — you'll need to re-enter those after importing, "+
				"especially on a different machine.\n\nContinue?",
			func(ok bool) {
				if !ok {
					return
				}
				if err := c.applySettingsImport(data); err != nil {
					dialog.ShowError(err, c.win)
					return
				}
				c.promptRestartAfterImport()
			}, c.win))
	}, c.win)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".zip"}))
	showDialog(fd)
}

// applySettingsImport writes every entry in zipData that matches a known
// settings file name to its real destination — anything else in the zip
// is silently ignored, so picking the wrong file harmlessly does nothing
// rather than scattering unrelated files across the config directories.
func (c *commander) applySettingsImport(zipData []byte) error {
	targets, err := c.settingsFilePaths()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return err
	}
	wrote := 0
	for _, f := range zr.File {
		dest, ok := targets[filepath.Base(f.Name)]
		if !ok {
			continue
		}
		if err := extractZipFileTo(f, dest); err != nil {
			return err
		}
		wrote++
	}
	if wrote == 0 {
		return errors.New("this file doesn't contain any recognized Commander settings")
	}
	return nil
}

func extractZipFileTo(f *zip.File, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// promptRestartAfterImport offers to quit right away — Quitting now (via
// the same quitApp teardown every other Quit entry point uses) is the only
// reliable way to guarantee the just-imported values actually take hold
// (see this file's top doc comment); reopening Commander afterward is on
// the user.
func (c *commander) promptRestartAfterImport() {
	showDialog(dialog.NewConfirm("Restart Required",
		"Settings imported. Quit "+appName+" now so the changes actually take "+
			"effect? (Reopen it afterward to see them.)",
		func(ok bool) {
			if ok {
				fyne.Do(func() { quitApp(c.app, c.win) })
			}
		}, c.win))
}
