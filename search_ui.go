// search_ui.go — recursive filename/pattern search within the active tab's
// current directory (File menu / F9 popup / pane toolbar 🔍 button).
// Results are a plain clickable list rather than a live-browsable results
// tab like Total Commander's — clicking a match opens its containing
// directory in a new tab in the same pane, with the match as the cursor
// row, which covers the same "search, then jump straight to it" workflow
// with far less new UI machinery.
package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"commander/internal/panelstate"
	"commander/internal/vfs/zipfs"
)

type searchMatch struct {
	path string // full path
	name string
}

// searchDepthOptions is every selectable depth limit, in menu order — "how
// many directory levels below the search root to descend into," so a
// search accidentally started somewhere huge (e.g. "/") can be bounded
// instead of running away. Persisted like Multi-Rename's settings: reopens
// with whatever was last chosen, not always resetting to Unlimited.
var searchDepthOptions = []string{"Unlimited", "Just this folder", "1 level deep", "2 levels deep", "3 levels deep", "5 levels deep", "10 levels deep"}

const prefSearchDepth = "searchDepth"

// searchDepthValue maps a searchDepthOptions label to the number of
// directory levels below root the walk may descend into (0 = root's direct
// contents only), or -1 for Unlimited.
func searchDepthValue(label string) int {
	switch label {
	case "Just this folder":
		return 0
	case "1 level deep":
		return 1
	case "2 levels deep":
		return 2
	case "3 levels deep":
		return 3
	case "5 levels deep":
		return 5
	case "10 levels deep":
		return 10
	default:
		return -1
	}
}

// showSearch prompts for a name or glob pattern and recursively searches p's
// active tab's current directory.
func (c *commander) showSearch(p *pane) {
	state := p.activeState()
	if state == nil {
		return
	}
	if view := p.activeView(); view != nil {
		if _, insideArchive := view.fs.(*zipfs.FS); insideArchive {
			c.showStatus("search isn't available inside archives")
			return
		}
	}
	root := state.Path

	patternEntry := newDialogEntry()
	patternEntry.SetPlaceHolder("Name or pattern, e.g. *.go or report")

	prefs := c.app.Preferences()
	depthSelect := widget.NewSelect(searchDepthOptions, func(label string) {
		prefs.SetString(prefSearchDepth, label)
	})
	depthSelect.SetSelected(prefs.StringWithFallback(prefSearchDepth, "Unlimited"))

	var matches []searchMatch
	var d dialog.Dialog

	resultsList := widget.NewList(
		func() int { return len(matches) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, o fyne.CanvasObject) {
			rel, err := filepath.Rel(root, matches[id].path)
			if err != nil {
				rel = matches[id].path
			}
			o.(*widget.Label).SetText(rel)
		},
	)
	resultsList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(matches) {
			return
		}
		m := matches[id]
		newState := panelstate.New(filepath.Dir(m.path))
		newState.Cursor = m.name
		// Inherit the current tab's view mode rather than panelstate.New's
		// own default (Brief, its zero value) — otherwise a match picked
		// while browsing in Full view would jump there in Brief instead.
		newState.ViewMode = state.ViewMode
		p.addTabFromState(newState)
		if v := p.activeView(); v != nil {
			v.ScrollToCursor()
		}
		d.Hide()
	}

	statusLbl := widget.NewLabel("")

	runSearch := func() {
		pattern := strings.TrimSpace(patternEntry.Text)
		if pattern == "" {
			return
		}
		maxDepth := searchDepthValue(depthSelect.Selected)
		matches = searchWalk(root, pattern, maxDepth, c.showHiddenFiles)
		statusLbl.SetText(fmt.Sprintf("%d match(es)", len(matches)))
		resultsList.Refresh()
	}
	patternEntry.OnSubmitted = func(string) { runSearch() }
	searchBtn := widget.NewButton("Search", runSearch)

	content := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, nil, searchBtn, patternEntry),
			container.NewHBox(widget.NewLabel("Depth:"), depthSelect),
			statusLbl,
		),
		nil, nil, nil,
		container.NewVScroll(resultsList),
	)

	d = dialog.NewCustom("Search "+root, "Close", content, c.win)
	d.Resize(fyne.NewSize(560, 420))
	showDialog(d)
	c.win.Canvas().Focus(patternEntry)
}

// matchesSearchPattern does a case-insensitive substring match for plain
// text, or a filepath.Match glob when pattern contains * or ? — covers both
// "just type part of the name" and "*.go"-style wildcard searches without
// pulling in a full regex engine.
func matchesSearchPattern(name, pattern string) bool {
	if strings.ContainsAny(pattern, "*?") {
		ok, err := filepath.Match(pattern, name)
		return err == nil && ok
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// searchWalk recursively finds every entry under root matching pattern,
// pruning whole subtrees (via filepath.SkipDir, not just filtering results
// after the fact — the actual point of maxDepth, so a search accidentally
// started somewhere huge doesn't walk it all anyway) once they're deeper
// than maxDepth levels below root (maxDepth < 0 means unlimited) or, unless
// showHidden, once a dotfile/dotdir is reached.
func searchWalk(root, pattern string, maxDepth int, showHidden bool) []searchMatch {
	var matches []searchMatch
	_ = filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if path == root {
			return nil
		}
		if maxDepth >= 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && strings.Count(rel, string(filepath.Separator)) > maxDepth {
				if de.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !showHidden && strings.HasPrefix(de.Name(), ".") {
			if de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if matchesSearchPattern(de.Name(), pattern) {
			matches = append(matches, searchMatch{path: path, name: de.Name()})
		}
		return nil
	})
	return matches
}

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
