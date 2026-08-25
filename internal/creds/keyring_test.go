package creds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestKeyringStoreRoundTrip(t *testing.T) {
	keyring.MockInit() // in-memory keyring so this runs without a real Secret Service

	store, err := NewStore()
	require.NoError(t, err, "NewStore")
	require.IsType(t, keyringStore{}, store, "expected keyringStore when keyring is available")

	v, err := store.Get(SourceToken)
	require.NoError(t, err)
	assert.Empty(t, v, "missing key should be empty")

	require.NoError(t, store.Set(SourceToken, "ghp_secret"))
	v, err = store.Get(SourceToken)
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", v)

	require.NoError(t, store.Delete(SourceToken))
	v, _ = store.Get(SourceToken)
	assert.Empty(t, v, "token present after delete")

	// Deleting a missing key is not an error.
	assert.NoError(t, store.Delete(SourceToken), "Delete of missing key")
}

func TestNewStoreForcesFileBackend(t *testing.T) {
	keyring.MockInit() // keyring is "available"...
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // ...but the env var forces the file store

	store, err := NewStore()
	require.NoError(t, err, "NewStore")
	assert.IsType(t, &fileStore{}, store, "expected *fileStore when forced")
}
