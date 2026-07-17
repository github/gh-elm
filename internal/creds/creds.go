// Package creds stores gh-elm's secret credentials (API tokens) behind a Store
// interface, keeping secrets out of the plaintext config file.
//
// NewStore selects the backend: the OS keyring (macOS Keychain, Linux Secret
// Service, Windows Credential Manager) when one is available, otherwise a 0600
// file in gh-elm's config dir. The file fallback matches gh's behavior in
// keyring-less environments such as Codespaces and CI.
package creds

import "os"

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
// GH_ELM_CREDENTIAL_STORE to "file" or "keyring" to force a specific backend.
func NewStore() (Store, error) {
	switch os.Getenv("GH_ELM_CREDENTIAL_STORE") {
	case "file":
		return newFileStore()
	case "keyring":
		return keyringStore{}, nil
	}
	if keyringAvailable() {
		return keyringStore{}, nil
	}
	return newFileStore()
}
