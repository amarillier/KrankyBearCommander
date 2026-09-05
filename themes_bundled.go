// themes_bundled.go — the starter panel-color themes shipped with the app
// (assets/themes/*.json), embedded so they work regardless of install
// location. A single directory embed rather than one fyne-bundle var per
// file (bundled.go's pattern) so adding another starter theme later is just
// dropping in another .json file, no regeneration step required. See
// internal/themes for the shared Theme type/loader and colors.go for the
// picker UI that lists these alongside a user's own saved themes.
package main

import (
	"embed"

	"commander/internal/themes"
)

//go:embed assets/themes/*.json
var bundledThemesFS embed.FS

// bundledThemes returns every starter theme shipped with the app, sorted
// by name (see themes.LoadAllFS).
func bundledThemes() []themes.Theme {
	return themes.LoadAllFS(bundledThemesFS, "assets/themes")
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
