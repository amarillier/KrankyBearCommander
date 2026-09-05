// Package themes persists named panel color themes (colors.go's
// ColorScheme, as hex strings) as individual JSON files — both the bundled
// starter themes shipped under assets/themes (read via an embedded fs.FS at
// the call site) and a user's own saved/imported themes (a real OS
// directory under UserDir). Has no Fyne dependency, matching this repo's
// other internal packages.
package themes

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Theme is one named panel color scheme. Colors are "#rrggbb" hex strings
// rather than image/color.Color so this package can encode/decode them with
// plain encoding/json — colors.go converts to/from its own ColorScheme.
type Theme struct {
	Name         string `json:"name"`
	PanelBG      string `json:"panelBG"`
	TextNormal   string `json:"textNormal"`
	TextSelected string `json:"textSelected"`
	TextCursor   string `json:"textCursor"`
	TextDir      string `json:"textDir"`
}

// UserDir returns the per-user directory custom (non-bundled) themes are
// saved to, namespaced by appName the same way favorites/layout/editors
// are (see internal/favorites.DefaultPath).
func UserDir(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "themes"), nil
}

// LoadAllFS reads every *.json file directly inside dir within fsys (an
// embedded assets/themes tree, or os.DirFS(UserDir) for user-saved themes)
// and parses each as a Theme. A malformed individual file is skipped rather
// than failing the whole load — one bad custom theme file shouldn't hide
// every other one. A missing dir is not an error: it returns an empty
// slice (a fresh install has no user themes yet). Results are sorted by
// Name.
func LoadAllFS(fsys fs.FS, dir string) []Theme {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var out []Theme
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t Theme
		if err := json.Unmarshal(b, &t); err != nil || strings.TrimSpace(t.Name) == "" {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// LoadAllUser is LoadAllFS over the real OS directory dir (see UserDir).
func LoadAllUser(dir string) []Theme {
	return LoadAllFS(os.DirFS(dir), ".")
}

// Load reads and parses a single theme file from an arbitrary OS path (an
// Import Theme… target, typically outside dir).
func Load(path string) (Theme, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, err
	}
	var t Theme
	if err := json.Unmarshal(b, &t); err != nil {
		return Theme{}, err
	}
	return t, nil
}

// Save writes t as JSON into dir (creating it if needed), named after a
// sanitized version of t.Name, and returns the path written. Saving under
// an unchanged Name overwrites that same file rather than accumulating
// duplicates, so re-"Save As"-ing after further tweaks updates in place.
func Save(dir string, t Theme) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, sanitizeFilename(t.Name)+".json")
	if err := Export(p, t); err != nil {
		return "", err
	}
	return p, nil
}

// Export writes t as JSON to an arbitrary destination path (an Export
// Theme… target) — same encoding as Save, just not namespaced under dir.
func Export(path string, t Theme) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9 _-]+`)

// sanitizeFilename turns an arbitrary theme Name into a safe file basename
// (letters/digits/space/underscore/hyphen only; everything else dropped),
// falling back to "theme" if that leaves nothing at all.
func sanitizeFilename(name string) string {
	s := strings.TrimSpace(unsafeFilenameChars.ReplaceAllString(name, ""))
	if s == "" {
		return "theme"
	}
	return s
}
