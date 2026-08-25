package creds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// TestClearAll proves reset clears secrets from BOTH persistent backends, not
// just the one NewStore would select. A token written to the keyring must not
// survive a reset that ran against the file backend, and vice versa.
func TestClearAll(t *testing.T) {
	keyring.MockInit() // in-memory keyring so keyringAvailable() is true here
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	fileStore, err := newFileStore()
	require.NoError(t, err, "newFileStore")

	// Seed each backend with a different token.
	require.NoError(t, fileStore.Set(SourceToken, "file-secret"), "file Set")
	require.NoError(t, (keyringStore{}).Set(TargetToken, "keyring-secret"), "keyring Set")

	require.NoError(t, ClearAll(SourceToken, TargetToken), "ClearAll")

	v, _ := fileStore.Get(SourceToken)
	assert.Empty(t, v, "file token survived ClearAll")
	v, _ = (keyringStore{}).Get(TargetToken)
	assert.Empty(t, v, "keyring token survived ClearAll")

	// Clearing again (everything already gone) must not error.
	assert.NoError(t, ClearAll(SourceToken, TargetToken), "ClearAll on empty backends")
}

func TestNewStoreRejectsInvalidOverride(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	t.Run("misspelled value is rejected", func(t *testing.T) {
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "fil")
		_, err := NewStore()
		require.Error(t, err, "expected an error for a bad override")
		assert.Contains(t, err.Error(), "GH_ELM_CREDENTIAL_STORE", "error should name the env var")
	})

	t.Run("explicit file is accepted", func(t *testing.T) {
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
		_, err := NewStore()
		assert.NoError(t, err, "NewStore(file)")
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
	require.NoError(t, os.WriteFile(credPath, []byte(`{}`), 0o644), "seed file")
	info, _ := os.Stat(credPath)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "seed file mode")

	store, err := NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(SourceToken, "secret"), "Set")

	info, err = os.Stat(credPath)
	require.NoError(t, err, "stat after write")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "file mode after write (should tighten permissive files)")
}
