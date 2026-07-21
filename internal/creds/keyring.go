package creds

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keyringService is the service name under which gh-elm stores secrets in the OS
// keyring. Individual tokens are stored under their credential key (e.g.
// SourceToken) as the account.
const keyringService = "gh-elm"

// keyringStore stores secrets in the OS keyring: macOS Keychain, the Linux
// Secret Service (GNOME Keyring / KWallet), or the Windows Credential Manager,
// via zalando/go-keyring. It requires no platform-specific code from us.
type keyringStore struct{}

func (keyringStore) Get(key string) (string, error) {
	v, err := keyring.Get(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (keyringStore) Set(key, value string) error {
	return keyring.Set(keyringService, key, value)
}

func (keyringStore) Delete(key string) error {
	err := keyring.Delete(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (keyringStore) Location() string {
	return "OS keyring (service " + keyringService + ")"
}

// keyringAvailable reports whether the OS keyring is usable. It performs a
// read-only probe: a missing key returns keyring.ErrNotFound (the keyring is
// reachable, the key just isn't set), whereas an unreachable keyring (no Secret
// Service / D-Bus, as in Codespaces and CI) returns a different error.
func keyringAvailable() bool {
	_, err := keyring.Get(keyringService, "__gh_elm_probe__")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}
