// filter_ui.go — incremental quick-filter (Ctrl+S, TotalCmd's convention):
// a small bar above the listing that narrows the current directory's rows
// live as you type, entirely client-side (no disk re-read).
package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"commander/internal/panelstate"
	"commander/internal/vfs"
)

// SetFilter updates this tab's incremental quick-filter query and
// re-renders immediately from the last full read (applyFilterAndSort) — no
// disk re-read, so typing is instant. Called on every keystroke from the
// filter bar.
func (v *fileListView) SetFilter(query string) {
	if v.state.Filter == query {
		return
	}
	prevRowCount := v.rowCount()
	v.state.Filter = query
	v.applyFilterAndSort(prevRowCount)
}

// applyFilterAndSort (re)builds v.entries — the currently displayed rows —
// from v.allEntries (the last full, hidden-files-filtered read) by applying
// the quick-filter query and the active sort, without touching disk.
// Shared by applyEntries (after a fresh Reload) and SetFilter (every
// keystroke), so typing to filter is instant.
func (v *fileListView) applyFilterAndSort(prevRowCount int) {
	entries := v.allEntries
	if v.state.Filter != "" {
		entries = filterEntries(entries, v.state.Filter)
	}
	v.entries = panelstate.SortEntries(entries, v.state.SortField, v.state.SortAscending)
	v.hasParent = v.fs.Dir(v.state.Path) != v.state.Path

	// See applyEntries' original doc comment (filelist.go): a
	// shrinking/growing row count — now also caused by the filter, not
	// just an external directory change — can leave widget.Table showing
	// a stale blank gap otherwise.
	if v.table != nil && v.rowCount() != prevRowCount {
		v.table.ScrollToTop()
	}

	v.ensureCursorVisible()
	v.refreshHeaderLabels()
	v.renderActiveView()
	v.reportSelection()
}

// ensureCursorVisible clears the cursor if it just fell out of v.entries
// (e.g. the quick filter narrowed past it) — otherwise Cursor would keep
// pointing at a row that's no longer rendered. Matches Navigate/Jump's own
// convention of an empty Cursor meaning "nothing focused yet" rather than
// forcing it onto some other row.
func (v *fileListView) ensureCursorVisible() {
	if v.state.Cursor == "" {
		return
	}
	if _, ok := v.rowIndexOf(v.state.Cursor); !ok {
		v.state.Cursor = ""
	}
}

// filterEntries returns a NEW slice of only the entries whose name
// contains query, case-insensitively. Unlike visibleEntries' in-place
// compaction (filelist.go), this must NOT mutate its input: v.allEntries
// is re-filtered from scratch on every keystroke, so compacting it in
// place would corrupt it for the next, less-restrictive keystroke (e.g.
// backspacing).
func filterEntries(entries []vfs.Entry, query string) []vfs.Entry {
	query = strings.ToLower(query)
	filtered := make([]vfs.Entry, 0, len(entries))
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), query) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// FilterVisible reports whether the quick-filter bar is currently shown —
// used by commander.toggleFilterActive (Ctrl+S) to decide whether to show
// or hide it.
func (v *fileListView) FilterVisible() bool {
	return v.filterBar != nil && v.filterBar.Visible()
}

// ShowFilterBar reveals and focuses this tab's quick-filter field.
// A no-op if Build() hasn't run yet.
func (v *fileListView) ShowFilterBar() {
	if v.filterBar == nil {
		return
	}
	v.filterBar.Show()
	if canvas := fyne.CurrentApp().Driver().CanvasForObject(v.filterField); canvas != nil {
		canvas.Focus(v.filterField)
	}
}

// HideFilterBar hides the quick-filter bar and clears the query — the
// filter field's own Escape handler and its clear button both use this, as
// does toggling Ctrl+S off.
func (v *fileListView) HideFilterBar() {
	if v.filterBar == nil {
		return
	}
	v.filterBar.Hide()
	v.filterField.SetText("")
	v.SetFilter("")
	if canvas := fyne.CurrentApp().Driver().CanvasForObject(v.filterField); canvas != nil {
		canvas.Unfocus()
	}
}

// filterEntry extends widget.Entry purely to add Escape-to-close, the same
// "extend the widget ourselves" pattern rename_ui.go's renameEntry and
// dialogesc_ui.go's dialogEntry already use for the same reason (Fyne's
// Entry has no public Escape hook — see CLAUDE.md's focused-widget-wins
// keyboard routing note).
type filterEntry struct {
	widget.Entry
	onCancel func()
}

func newFilterEntry(onChanged func(string), onCancel func()) *filterEntry {
	e := &filterEntry{onCancel: onCancel}
	e.ExtendBaseWidget(e)
	e.OnChanged = onChanged
	return e
}

func (e *filterEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if e.onCancel != nil {
			e.onCancel()
		}
		return
	}
	e.Entry.TypedKey(ev)
}
