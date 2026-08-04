package launchers

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not be an error, got %v", err)
	}
	if len(c.Launchers) != 0 {
		t.Fatalf("expected no launchers, got %+v", c.Launchers)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "launchers.json")
	want := Config{Launchers: []Launcher{{Name: "VS Code", Command: "code"}, {Name: "Vim", Command: "vim"}}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Launchers) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestAddReplacesExistingByName(t *testing.T) {
	var c Config
	c.Add("VS Code", "code")
	c.Add("VS Code", "/usr/local/bin/code")

	if len(c.Launchers) != 1 {
		t.Fatalf("expected Add with the same name to replace, got %+v", c.Launchers)
	}
	if c.Launchers[0].Command != "/usr/local/bin/code" {
		t.Fatalf("expected replaced command, got %+v", c.Launchers[0])
	}
}

func TestUpsertAddsThenReplacesFullEntryByName(t *testing.T) {
	var c Config
	c.Upsert(Launcher{Name: "VS Code", Command: "code"})
	c.Upsert(Launcher{Name: "VS Code", Command: "/usr/local/bin/code", Args: "--wait", WorkingDir: "/tmp", Env: "FOO=bar"})

	if len(c.Launchers) != 1 {
		t.Fatalf("expected Upsert with the same name to replace, got %+v", c.Launchers)
	}
	got := c.Launchers[0]
	if got.Command != "/usr/local/bin/code" || got.Args != "--wait" || got.WorkingDir != "/tmp" || got.Env != "FOO=bar" {
		t.Fatalf("expected the full entry replaced, got %+v", got)
	}
}

func TestFind(t *testing.T) {
	var c Config
	c.Add("Vim", "vim")

	l, ok := c.Find("Vim")
	if !ok || l.Command != "vim" {
		t.Fatalf("Find(Vim) = %+v, %v", l, ok)
	}
	if _, ok := c.Find("Nope"); ok {
		t.Fatal("Find should report false for an unconfigured name")
	}
}

func TestRemove(t *testing.T) {
	var c Config
	c.Add("Vim", "vim")
	c.Add("Nano", "nano")

	c.Remove("Vim")

	if len(c.Launchers) != 1 || c.Launchers[0].Name != "Nano" {
		t.Fatalf("expected only Nano to remain, got %+v", c.Launchers)
	}
}
