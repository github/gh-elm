package creds

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeyringStoreRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keyring so this runs without a real Secret Service

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, ok := store.(keyringStore); !ok {
		t.Fatalf("expected keyringStore when keyring is available, got %T", store)
	}

	if v, err := store.Get(SourceToken); err != nil || v != "" {
		t.Fatalf("Get missing = (%q, %v), want empty", v, err)
	}
	if err := store.Set(SourceToken, "ghp_secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := store.Get(SourceToken); err != nil || v != "ghp_secret" {
		t.Fatalf("Get = (%q, %v), want ghp_secret", v, err)
	}
	if err := store.Delete(SourceToken); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if v, _ := store.Get(SourceToken); v != "" {
		t.Errorf("token present after delete: %q", v)
	}
	// Deleting a missing key is not an error.
	if err := store.Delete(SourceToken); err != nil {
		t.Errorf("Delete of missing key = %v, want nil", err)
	}
}

func TestNewStoreForcesFileBackend(t *testing.T) {
	keyring.MockInit() // keyring is "available"...
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // ...but the env var forces the file store

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, ok := store.(*fileStore); !ok {
		t.Fatalf("expected *fileStore when forced, got %T", store)
	}
}
