// Package connections persists a user-managed list of saved remote
// connections (Connections manager — see connections_ui.go), the same shape
// internal/favorites and internal/editors already use for their own
// persisted lists. It has no Fyne dependency, matching this repo's other
// internal packages.
//
// Only non-secret fields live here, in plain JSON: password/key-passphrase
// are never written to disk by this package at all — see keychain.go, which
// stores exactly one secret per Connection (keyed by ID, not Name, so
// renaming a connection never orphans its stored secret) in the OS keychain
// instead. TrustedHostKeyFingerprint isn't secret (it's the whole point of
// SSH host-key trust-on-first-use that it's not), so it's stored here.
package connections

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// Connection describes one saved remote connection.
type Connection struct {
	ID       string // random, generated once at creation — see NewID
	Name     string
	Protocol string // "sftp", "smb", or "fileagent"
	Host     string
	Port     int
	// Username is unused for "fileagent" — that protocol authenticates with
	// a pre-shared key alone (see Connection's own "secret", stored via
	// internal/connections/keychain.go), no username of any kind.
	Username string
	// RemotePath is where a tab opened against this connection starts
	// browsing — not necessarily the remote filesystem's own root. For
	// "smb", this also carries the share name as its first path component
	// (e.g. "Users" or "Users/allan") — see smbfs.parseShareAndPath. For
	// "fileagent", it's a plain "/"-rooted virtual path on the agent's own
	// shared root (whatever real directory the agent was started against).
	RemotePath string
	// SSHKeyPath, if set, is tried before password auth (see sftpfs.Connect).
	// "sftp" only.
	SSHKeyPath string
	// Domain is the NTLM domain/workgroup for "smb" auth — optional, most
	// setups (a local account, a NAS, a workgroup machine) leave it empty.
	// "smb" only.
	Domain string
	// TrustedHostKeyFingerprint is the SHA-256 hex fingerprint accepted on a
	// prior successful connect (SSH trust-on-first-use) — empty until the
	// first connect, updated only through an explicit "trust this host key"
	// confirmation, never silently overwritten on a mismatch. "sftp" only —
	// SMB has no equivalent host-identity primitive at this level.
	TrustedHostKeyFingerprint string
	// FileAgentTLSPin is the SHA-256 hex fingerprint of the FileAgent
	// listener's TLS certificate, entered directly by the user (copied from
	// wherever the listener printed it on startup) — unlike SFTP's host-key
	// trust-on-first-use, there's no probe-then-prompt step here; the pin is
	// expected to already be known out of band before the first connect.
	// "fileagent" only.
	FileAgentTLSPin string
}

// Config is the persisted set of connections.
type Config struct {
	Connections []Connection `json:"connections"`
}

// NewID returns a fresh random identifier for a new Connection — 16 random
// bytes as hex, matching what a keychain account name needs (stable, and
// never reused across two different connections even if both are later
// renamed to the same Name).
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing means the OS's CSPRNG is unavailable —
		// exceptionally rare, and a duplicate/predictable ID here would only
		// ever risk two connections sharing a keychain entry, not a security
		// bypass on its own; fall back to a fixed, still-syntactically-valid
		// value rather than panicking over what would already be a bigger
		// problem with the host.
		return "0000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// DefaultPath returns the per-user path connections.json lives at, namespaced
// by appName the same way favorites.DefaultPath/editors.DefaultPath are.
func DefaultPath(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "connections.json"), nil
}

// Load reads and parses path. A missing file is not an error: it returns an
// empty Config so first-run callers see no saved connections.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes cfg to path as JSON, creating parent directories as needed.
func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Remove drops the connection with the given ID from cfg, if present.
func (cfg *Config) Remove(id string) {
	out := cfg.Connections[:0]
	for _, c := range cfg.Connections {
		if c.ID != id {
			out = append(out, c)
		}
	}
	cfg.Connections = out
}

// Upsert adds conn, or replaces the existing entry with the same ID.
func (cfg *Config) Upsert(conn Connection) {
	for i, c := range cfg.Connections {
		if c.ID == conn.ID {
			cfg.Connections[i] = conn
			return
		}
	}
	cfg.Connections = append(cfg.Connections, conn)
}
