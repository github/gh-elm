package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreSetGetDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	store, err := newFileStore()
	if err != nil {
		t.Fatalf("newFileStore: %v", err)
	}

	// Missing key returns empty, no error.
	if v, err := store.Get(SourceToken); err != nil || v != "" {
		t.Fatalf("Get missing = (%q, %v), want empty", v, err)
	}

	if err := store.Set(SourceToken, "ghp_secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := store.Get(SourceToken); err != nil || v != "ghp_secret" {
		t.Fatalf("Get = (%q, %v), want ghp_secret", v, err)
	}

	// Independent keys don't collide.
	if err := store.Set(TargetToken, "ghp_target"); err != nil {
		t.Fatalf("Set target: %v", err)
	}
	if v, _ := store.Get(SourceToken); v != "ghp_secret" {
		t.Errorf("source token changed after setting target: %q", v)
	}

	// Delete is idempotent.
	if err := store.Delete(SourceToken); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if v, _ := store.Get(SourceToken); v != "" {
		t.Errorf("token still present after delete: %q", v)
	}
	if err := store.Delete(SourceToken); err != nil {
		t.Errorf("Delete of missing key should be nil, got %v", err)
	}
}

func TestFileStorePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	store, err := newFileStore()
	if err != nil {
		t.Fatalf("newFileStore: %v", err)
	}
	if err := store.Set(SourceToken, "x"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials file perm = %o, want 600", perm)
	}
}
