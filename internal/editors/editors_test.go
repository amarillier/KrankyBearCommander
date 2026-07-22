package editors

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileDefaultsToBuiltin(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if c.Default != BuiltinName {
		t.Fatalf("Default = %q, want %q", c.Default, BuiltinName)
	}
	if len(c.Editors) != 0 {
		t.Fatalf("expected no editors, got %+v", c.Editors)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "editors.json")
	want := Config{
		Editors: []Editor{{Name: "VS Code", Command: "code"}, {Name: "Vim", Command: "vim"}},
		Default: "VS Code",
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Default != "VS Code" || len(got.Editors) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestAddReplacesExistingByName(t *testing.T) {
	var c Config
	c.Add("VS Code", "code")
	c.Add("VS Code", "/usr/local/bin/code")

	if len(c.Editors) != 1 {
		t.Fatalf("expected Add with the same name to replace, got %+v", c.Editors)
	}
	if c.Editors[0].Command != "/usr/local/bin/code" {
		t.Fatalf("expected replaced command, got %+v", c.Editors[0])
	}
}

func TestFind(t *testing.T) {
	var c Config
	c.Add("Vim", "vim")

	e, ok := c.Find("Vim")
	if !ok || e.Command != "vim" {
		t.Fatalf("Find(Vim) = %+v, %v", e, ok)
	}
	if _, ok := c.Find("Nope"); ok {
		t.Fatal("Find should report false for an unconfigured name")
	}
}

func TestRemoveResetsDefaultWhenRemovingIt(t *testing.T) {
	c := Config{Default: "Vim"}
	c.Add("Vim", "vim")
	c.Add("Nano", "nano")

	c.Remove("Vim")

	if len(c.Editors) != 1 || c.Editors[0].Name != "Nano" {
		t.Fatalf("expected only Nano to remain, got %+v", c.Editors)
	}
	if c.Default != BuiltinName {
		t.Fatalf("Default = %q, want reset to %q after removing it", c.Default, BuiltinName)
	}
}

func TestRemoveLeavesOtherDefaultAlone(t *testing.T) {
	c := Config{Default: "Nano"}
	c.Add("Vim", "vim")
	c.Add("Nano", "nano")

	c.Remove("Vim")

	if c.Default != "Nano" {
		t.Fatalf("Default = %q, want unchanged Nano", c.Default)
	}
}
