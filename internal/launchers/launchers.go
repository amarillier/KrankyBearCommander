// Package launchers persists the Application Launcher's list of
// user-added applications (name + a command to run, no arguments — see
// internal/launch.Run). No Fyne dependency, matching this repo's other
// internal packages; near-identical in shape to internal/editors, minus
// the "current default" concept editors has (a launcher entry is always
// just "click it, it runs").
package launchers

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Launcher is one configured application. Args/WorkingDir/Env are all
// optional, stored as user-typed text (matching every other free-text
// field in this app) and only parsed at launch time — see internal/
// launch.SplitArgs and launcher_ui.go's parseEnvLines.
type Launcher struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	// Args is the raw parameters text, split at launch time so quoted
	// segments (e.g. `--title "My App"`) stay one argument.
	Args string `json:"args,omitempty"`
	// WorkingDir is the directory the launched process starts in ("Start
	// In") — defaults to the command's own directory when first added
	// (see launcher_ui.go), but editable/clearable afterward.
	WorkingDir string `json:"workingDir,omitempty"`
	// Env is one KEY=VALUE per line, appended to (not replacing) the
	// launched process's inherited environment.
	Env string `json:"env,omitempty"`
}

// Config is the persisted set of launchers.
type Config struct {
	Launchers []Launcher `json:"launchers"`
}

// DefaultPath returns the per-user path launchers.json lives at, namespaced
// by appName the same way internal/editors/internal/favorites namespace theirs.
func DefaultPath(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "launchers.json"), nil
}

// Load reads and parses path. A missing file is not an error: it returns an
// empty Config so first-run callers see no configured launchers.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes c to path as JSON, creating parent directories as needed.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Find looks up a configured launcher by name.
func (c Config) Find(name string) (Launcher, bool) {
	for _, l := range c.Launchers {
		if l.Name == name {
			return l, true
		}
	}
	return Launcher{}, false
}

// Add appends a new launcher, or replaces the command of an existing one
// with the same name.
func (c *Config) Add(name, command string) {
	for i, l := range c.Launchers {
		if l.Name == name {
			c.Launchers[i].Command = command
			return
		}
	}
	c.Launchers = append(c.Launchers, Launcher{Name: name, Command: command})
}

// Upsert adds l, or replaces the existing entry with the same Name —
// used by the full Add/Edit form (see launcher_ui.go), which can set
// every field at once, unlike Add's simpler Name+Command-only replace.
func (c *Config) Upsert(l Launcher) {
	for i, existing := range c.Launchers {
		if existing.Name == l.Name {
			c.Launchers[i] = l
			return
		}
	}
	c.Launchers = append(c.Launchers, l)
}

// Remove drops the named launcher.
func (c *Config) Remove(name string) {
	out := c.Launchers[:0]
	for _, l := range c.Launchers {
		if l.Name != name {
			out = append(out, l)
		}
	}
	c.Launchers = out
}
