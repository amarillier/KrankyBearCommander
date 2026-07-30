package connections

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keychainService namespaces this app's keychain entries (macOS Keychain,
// Windows Credential Manager, Linux Secret Service) from anything else on
// the same machine. Reuses the app's own display name rather than a
// separate constant, matching how favorites/editors/connections.json are
// all namespaced by the same appName.
const keychainService = "KrankyBear Commander"

// SetSecret stores (or replaces) id's secret — a password or SSH key
// passphrase, whichever the connection's auth uses. Keyed by Connection.ID,
// not Name, so renaming a connection never orphans its stored secret.
func SetSecret(id, secret string) error {
	return keyring.Set(keychainService, id, secret)
}

// GetSecret returns id's stored secret and whether one was found. A missing
// entry (or any keychain error, e.g. no Secret Service daemon on a headless
// Linux box) is reported as "not found" so callers cleanly treat it as
// "no secret saved" rather than a hard failure.
func GetSecret(id string) (string, bool) {
	secret, err := keyring.Get(keychainService, id)
	if err != nil {
		return "", false
	}
	return secret, true
}

// DeleteSecret removes id's stored secret, if any — called when a
// connection is deleted. A missing entry is treated as success.
func DeleteSecret(id string) error {
	if err := keyring.Delete(keychainService, id); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
