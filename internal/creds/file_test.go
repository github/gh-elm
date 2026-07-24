package creds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStoreSetGetDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	store, err := newFileStore()
	require.NoError(t, err, "newFileStore")

	// Missing key returns empty, no error.
	v, err := store.Get(SourceToken)
	require.NoError(t, err)
	assert.Empty(t, v, "missing key should be empty")

	require.NoError(t, store.Set(SourceToken, "ghp_secret"))
	v, err = store.Get(SourceToken)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", v)

	// Independent keys don't collide.
	require.NoError(t, store.Set(TargetToken, "ghp_target"))
	v, _ = store.Get(SourceToken)
	assert.Equal(t, "ghp_secret", v, "source token should not change after setting target")

	// Delete is idempotent.
	require.NoError(t, store.Delete(SourceToken))
	v, _ = store.Get(SourceToken)
	assert.Empty(t, v, "token still present after delete")
	assert.NoError(t, store.Delete(SourceToken), "Delete of missing key should be nil")
}

func TestFileStorePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	store, err := newFileStore()
	require.NoError(t, err, "newFileStore")
	require.NoError(t, store.Set(SourceToken, "x"))

	info, err := os.Stat(filepath.Join(dir, "credentials.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "credentials file perm")
}
