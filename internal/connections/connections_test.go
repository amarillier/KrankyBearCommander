package connections

import (
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	cfg := Config{Connections: []Connection{
		{ID: NewID(), Name: "Home Server", Protocol: "sftp", Host: "example.com", Port: 22, Username: "allan", RemotePath: "/home/allan"},
	}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Connections) != 1 || got.Connections[0] != cfg.Connections[0] {
		t.Fatalf("got %+v, want %+v", got.Connections, cfg.Connections)
	}
}

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Connections) != 0 {
		t.Fatalf("got %+v, want empty", cfg.Connections)
	}
}

func TestUpsertAddsThenReplacesByID(t *testing.T) {
	var cfg Config
	a := Connection{ID: "a", Name: "First"}
	cfg.Upsert(a)
	if len(cfg.Connections) != 1 {
		t.Fatalf("got %d connections, want 1", len(cfg.Connections))
	}
	renamed := Connection{ID: "a", Name: "Renamed"}
	cfg.Upsert(renamed)
	if len(cfg.Connections) != 1 || cfg.Connections[0].Name != "Renamed" {
		t.Fatalf("got %+v, want a single entry named Renamed", cfg.Connections)
	}
	b := Connection{ID: "b", Name: "Second"}
	cfg.Upsert(b)
	if len(cfg.Connections) != 2 {
		t.Fatalf("got %d connections, want 2", len(cfg.Connections))
	}
}

func TestRemoveDropsOnlyMatchingID(t *testing.T) {
	cfg := Config{Connections: []Connection{{ID: "a"}, {ID: "b"}}}
	cfg.Remove("a")
	if len(cfg.Connections) != 1 || cfg.Connections[0].ID != "b" {
		t.Fatalf("got %+v, want only id b left", cfg.Connections)
	}
}

func TestNewIDIsUniqueAndNonEmpty(t *testing.T) {
	a, b := NewID(), NewID()
	if a == "" || b == "" {
		t.Fatal("NewID should never return an empty string")
	}
	if a == b {
		t.Fatal("two calls to NewID should not collide")
	}
}

func TestSecretRoundTrip(t *testing.T) {
	keyring.MockInit()
	if _, ok := GetSecret("no-such-id"); ok {
		t.Fatal("GetSecret for an id with no stored secret should report not-found")
	}
	if err := SetSecret("conn-1", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	secret, ok := GetSecret("conn-1")
	if !ok || secret != "s3cr3t" {
		t.Fatalf("GetSecret = (%q, %v), want (s3cr3t, true)", secret, ok)
	}
	if err := DeleteSecret("conn-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := GetSecret("conn-1"); ok {
		t.Fatal("secret should be gone after DeleteSecret")
	}
	// Deleting an already-missing entry is success, not an error.
	if err := DeleteSecret("conn-1"); err != nil {
		t.Fatalf("DeleteSecret of an already-missing entry should succeed, got %v", err)
	}
}
