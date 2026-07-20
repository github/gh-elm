package creds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestClearAll proves reset clears secrets from BOTH persistent backends, not
// just the one NewStore would select. A token written to the keyring must not
// survive a reset that ran against the file backend, and vice versa.
func TestClearAll(t *testing.T) {
	keyring.MockInit() // in-memory keyring so keyringAvailable() is true here
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	fileStore, err := newFileStore()
	if err != nil {
		t.Fatalf("newFileStore: %v", err)
	}

	// Seed each backend with a different token.
	if err := fileStore.Set(SourceToken, "file-secret"); err != nil {
		t.Fatalf("file Set: %v", err)
	}
	if err := (keyringStore{}).Set(TargetToken, "keyring-secret"); err != nil {
		t.Fatalf("keyring Set: %v", err)
	}

	if err := ClearAll(SourceToken, TargetToken); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	if v, _ := fileStore.Get(SourceToken); v != "" {
		t.Errorf("file token survived ClearAll: %q", v)
	}
	if v, _ := (keyringStore{}).Get(TargetToken); v != "" {
		t.Errorf("keyring token survived ClearAll: %q", v)
	}

	// Clearing again (everything already gone) must not error.
	if err := ClearAll(SourceToken, TargetToken); err != nil {
		t.Errorf("ClearAll on empty backends = %v, want nil", err)
	}
}

func TestNewStoreRejectsInvalidOverride(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	t.Run("misspelled value is rejected", func(t *testing.T) {
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "fil")
		store, err := NewStore()
		if err == nil {
			t.Fatalf("expected an error for a bad override, got store %T", store)
		}
		if !strings.Contains(err.Error(), "GH_ELM_CREDENTIAL_STORE") {
			t.Errorf("error should name the env var, got: %v", err)
		}
	})

	t.Run("explicit file is accepted", func(t *testing.T) {
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
		if _, err := NewStore(); err != nil {
			t.Fatalf("NewStore(file) = %v, want nil", err)
		}
	})
}

// TestFileStoreTightensPermissions proves that writing to a pre-existing file
// with permissive permissions (e.g. 0644) tightens it to 0600, so tokens aren't
// left world-readable if the file was restored from backup or miscreated.
func TestFileStoreTightensPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", tmpDir)
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	credPath := filepath.Join(tmpDir, "credentials.json")

	// Pre-create the file with permissive mode.
	if err := os.WriteFile(credPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	info, _ := os.Stat(credPath)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("seed file mode = %o, want 0644", info.Mode().Perm())
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Set(SourceToken, "secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err = os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat after write: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode after write = %o, want 0600 (should tighten permissive files)", got)
	}
}
