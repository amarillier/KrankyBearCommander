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
)

type searchMatch struct {
	path string // full path
	name string
}

// showSearch prompts for a name or glob pattern and recursively searches p's
// active tab's current directory.
func (c *commander) showSearch(p *pane) {
	state := p.activeState()
	if state == nil {
		return
	}
	root := state.Path

	patternEntry := widget.NewEntry()
	patternEntry.SetPlaceHolder("Name or pattern, e.g. *.go or report")

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
		p.addTabFromState(newState)
		d.Hide()
	}

	statusLbl := widget.NewLabel("")

	runSearch := func() {
		pattern := strings.TrimSpace(patternEntry.Text)
		if pattern == "" {
			return
		}
		matches = matches[:0]
		showHidden := c.showHiddenFiles
		_ = filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries, keep walking
			}
			if path == root {
				return nil
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
		statusLbl.SetText(fmt.Sprintf("%d match(es)", len(matches)))
		resultsList.Refresh()
	}
	patternEntry.OnSubmitted = func(string) { runSearch() }
	searchBtn := widget.NewButton("Search", runSearch)

	content := container.NewBorder(
		container.NewVBox(
			container.NewBorder(nil, nil, nil, searchBtn, patternEntry),
			statusLbl,
		),
		nil, nil, nil,
		container.NewVScroll(resultsList),
	)

	d = dialog.NewCustom("Search "+root, "Close", content, c.win)
	d.Resize(fyne.NewSize(560, 420))
	d.Show()
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

// "Now this is not the end. It is not even the beginning of the end. But it is, perhaps, the end of the beginning." Winston Churchill, November 10, 1942
