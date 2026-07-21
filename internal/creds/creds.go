// Package creds stores gh-elm's secret credentials (API tokens) behind a Store
// interface, keeping secrets out of the plaintext config file.
//
// NewStore selects the backend: the OS keyring (macOS Keychain, Linux Secret
// Service, Windows Credential Manager) when one is available, otherwise a 0600
// file in gh-elm's config dir. The file fallback matches gh's behavior in
// keyring-less environments such as Codespaces and CI.
package creds

import (
	"errors"
	"fmt"
	"os"
)

// Credential keys.
const (
	SourceToken = "source-token"
	TargetToken = "target-token"
)

// Store persists and retrieves secret credentials by key.
type Store interface {
	// Get returns the stored value for key, or "" (and no error) if unset.
	Get(key string) (string, error)
	// Set stores value under key.
	Set(key, value string) error
	// Delete removes key. Deleting a missing key is not an error.
	Delete(key string) error
	// Location returns a human-readable description of where secrets are stored,
	// for user-facing messages.
	Location() string
}

// NewStore returns the default credential store. It prefers the OS keyring and
// falls back to the 0600 file store when no keyring is available. Set
// GH_ELM_CREDENTIAL_STORE to "file" or "keyring" to force a specific backend; any
// other non-empty value is rejected so a typo can't silently change where tokens
// are written.
func NewStore() (Store, error) {
	switch v := os.Getenv("GH_ELM_CREDENTIAL_STORE"); v {
	case "":
		if keyringAvailable() {
			return keyringStore{}, nil
		}
		return newFileStore()
	case "file":
		return newFileStore()
	case "keyring":
		return keyringStore{}, nil
	default:
		return nil, fmt.Errorf("invalid GH_ELM_CREDENTIAL_STORE %q: must be %q, %q, or unset", v, "file", "keyring")
	}
}

// ClearAll removes the given keys from every persistent backend gh-elm might
// have written to: the 0600 file store (always) and the OS keyring (when one is
// available). NewStore's choice can differ between runs — the user can force
// GH_ELM_CREDENTIAL_STORE, and automatic selection depends on keyring
// availability — so clearing only the currently-selected backend can leave
// secrets behind in the other. Deleting a missing key is not an error, and an
// unavailable keyring is skipped rather than failing.
func ClearAll(keys ...string) error {
	fileStore, err := newFileStore()
	if err != nil {
		return err
	}

	stores := []Store{fileStore}
	if keyringAvailable() {
		stores = append(stores, keyringStore{})
	}

	var errs []error
	for _, st := range stores {
		for _, key := range keys {
			if err := st.Delete(key); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", st.Location(), err))
			}
		}
	}
	return errors.Join(errs...)
}
