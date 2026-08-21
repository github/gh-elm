package endpoints

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

func newTestResolver(t *testing.T) *Resolver {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // Force file backend to avoid real keyring.
	r, err := NewResolver()
	require.NoError(t, err, "NewResolver")
	return r
}

func TestSourcePrecedence(t *testing.T) {
	// Stored config + credentials are the lowest precedence.
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file") // Force file backend to avoid polluting real keyring.
	require.NoError(t, (&config.Config{SourceURL: "https://stored"}).Save())
	store, err := creds.NewStore()
	require.NoError(t, err)
	require.NoError(t, store.Set(creds.SourceToken, "stored-token"))

	r, err := NewResolver()
	require.NoError(t, err)

	// Stored only.
	ep, err := r.Source("", "")
	require.NoError(t, err)
	assert.Equal(t, "https://stored/api/v3", ep.URL)
	assert.Equal(t, "stored-token", ep.Token)

	// Env overrides stored.
	t.Setenv(config.EnvSourceURL, "https://env")
	t.Setenv(config.EnvSourceToken, "env-token")
	ep, _ = r.Source("", "")
	assert.Equal(t, "https://env/api/v3", ep.URL, "env should override stored")
	assert.Equal(t, "env-token", ep.Token)

	// Flag overrides env.
	ep, _ = r.Source("https://flag", "flag-token")
	assert.Equal(t, "https://flag/api/v3", ep.URL, "flag should override env")
	assert.Equal(t, "flag-token", ep.Token)
}

func TestSourceNormalizesBareHost(t *testing.T) {
	r := newTestResolver(t)

	t.Run("adds scheme and REST path to a bare host", func(t *testing.T) {
		ep, err := r.Source("ghes.example.com", "tok")

		require.NoError(t, err)
		assert.Equal(t, "https://ghes.example.com/api/v3", ep.URL)
	})

	t.Run("preserves an explicit REST path", func(t *testing.T) {
		ep, err := r.Source("https://ghes.example.com/api/v3", "tok")

		require.NoError(t, err)
		assert.Equal(t, "https://ghes.example.com/api/v3", ep.URL)
	})

	t.Run("removes a trailing backslash", func(t *testing.T) {
		ep, err := r.Source("https://ghes.example.com\\", "tok")

		require.NoError(t, err)
		assert.Equal(t, "https://ghes.example.com/api/v3", ep.URL)
	})

	t.Run("removes an escaped trailing slash", func(t *testing.T) {
		ep, err := r.Source("https://ghes.example.com\\/", "tok")

		require.NoError(t, err)
		assert.Equal(t, "https://ghes.example.com/api/v3", ep.URL)
	})
}

func TestTargetResolvesIndependently(t *testing.T) {
	r := newTestResolver(t)
	t.Setenv(config.EnvTargetURL, "https://target-env")
	t.Setenv(config.EnvTargetToken, "target-env-token")

	ep, err := r.Target("", "")
	require.NoError(t, err)
	assert.Equal(t, "https://target-env", ep.URL)
	assert.Equal(t, "target-env-token", ep.Token)
}

func TestTargetNormalizesTrailingBackslash(t *testing.T) {
	r := newTestResolver(t)

	ep, err := r.Target("https://target.example.com\\", "tok")

	require.NoError(t, err)
	assert.Equal(t, "https://target.example.com", ep.URL)
}
