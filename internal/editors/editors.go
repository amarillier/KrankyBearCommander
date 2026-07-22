// Package editors persists the F4 "edit" preference: the built-in editor, or
// one of a user-configured list of external editors (name + command path;
// the file path is appended as the launched command's last argument). No
// Fyne dependency, matching this repo's other internal packages.
package editors

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BuiltinName is the sentinel Default value meaning "use the app's built-in
// text editor" rather than one of Editors.
const BuiltinName = "Built-in"

// Editor is one configured external editor.
type Editor struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Config is the persisted editor preference: the configured external
// editors, and which one (or BuiltinName) F4 uses by default.
type Config struct {
	Editors []Editor `json:"editors"`
	Default string   `json:"default"`
}

// DefaultPath returns the per-user path editors.json lives at, namespaced by
// appName the same way internal/layout/internal/favorites namespace theirs.
func DefaultPath(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "editors.json"), nil
}

// Load reads and parses path. A missing file is not an error: it returns a
// Config defaulting to the built-in editor with no external editors
// configured, for first-run callers.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Default: BuiltinName}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.Default == "" {
		c.Default = BuiltinName
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

// Find looks up a configured external editor by name.
func (c Config) Find(name string) (Editor, bool) {
	for _, e := range c.Editors {
		if e.Name == name {
			return e, true
		}
	}
	return Editor{}, false
}

// Add appends a new external editor, or replaces the command of an existing
// one with the same name.
func (c *Config) Add(name, command string) {
	for i, e := range c.Editors {
		if e.Name == name {
			c.Editors[i].Command = command
			return
		}
	}
	c.Editors = append(c.Editors, Editor{Name: name, Command: command})
}

// Remove drops the named external editor, resetting Default to BuiltinName
// if that editor was the default.
func (c *Config) Remove(name string) {
	out := c.Editors[:0]
	for _, e := range c.Editors {
		if e.Name != name {
			out = append(out, e)
		}
	}
	c.Editors = out
	if c.Default == name {
		c.Default = BuiltinName
	}
}
