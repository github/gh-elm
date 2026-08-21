package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

// failingStore is a creds.Store whose Get always fails, simulating an unreadable
// or malformed credential backend.
type failingStore struct{ err error }

func (f failingStore) Get(string) (string, error) { return "", f.err }
func (failingStore) Set(string, string) error     { return nil }
func (failingStore) Delete(string) error          { return nil }
func (failingStore) Location() string             { return "test backend" }

func TestConfigureShowPropagatesStoreError(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := runConfigureShow(cmd, failingStore{err: errors.New("malformed credentials file")})
	require.Error(t, err, "expected an error when the credential store cannot be read")
	assert.Contains(t, err.Error(), "malformed credentials file", "error should surface the backend failure")
	assert.NotContains(t, buf.String(), "not set", "a read failure must not be reported as \"not set\"")
}

// execConfigure runs `gh elm config <args>` through the root command and
// returns combined output. Callers set GH_ELM_CONFIG_DIR / GH_ELM_CREDENTIAL_STORE
// (and seed state) beforehand so the run is isolated to a temp dir.
func execConfigure(args ...string) (string, error) {
	root := NewRootCmd("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"config"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func seedFileStore(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
}

func TestConfigureShow(t *testing.T) {
	seedFileStore(t)
	require.NoError(t, (&config.Config{SourceURL: "https://ghes.example.com"}).Save(), "seed config")
	store, err := creds.NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(creds.SourceToken, "ghp_source"), "seed token")

	out, err := execConfigure("show")
	require.NoError(t, err, "config show")
	assert.False(t, strings.HasPrefix(out, "\n"))
	assert.True(t, strings.HasSuffix(out, "\n\n"))
	assert.Contains(t, out, "https://ghes.example.com", "missing source url in output")
	assert.Contains(t, out, "set (hidden)", "seeded source token should read as set")
	assert.Contains(t, out, "not set", "unset target token should read as not set")
}

func TestConfigureCompatibility(t *testing.T) {
	t.Run("legacy configure --show still works", func(t *testing.T) {
		seedFileStore(t)

		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs([]string{"configure", "--show"})

		require.NoError(t, root.Execute())
		assert.Contains(t, output.String(), "Source (GHES)")
	})

	t.Run("legacy configure is hidden from root help", func(t *testing.T) {
		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"--help"})

		require.NoError(t, root.Execute())
		assert.NotContains(t, output.String(), "  configure:")
	})

	t.Run("legacy mode flags are hidden from config help", func(t *testing.T) {
		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"config", "--help"})

		require.NoError(t, root.Execute())
		assert.NotContains(t, output.String(), "--show")
		assert.NotContains(t, output.String(), "--reset")
	})
}

func TestConfigureReset(t *testing.T) {
	seedFileStore(t)
	require.NoError(t, (&config.Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}).Save(), "seed config")
	store, err := creds.NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(creds.SourceToken, "ghp_source"))
	require.NoError(t, store.Set(creds.TargetToken, "ghp_target"))

	out, err := execConfigure("reset")
	require.NoError(t, err, "config reset")
	assert.False(t, strings.HasPrefix(out, "\n"))
	assert.True(t, strings.HasSuffix(out, "\n\n"))
	assert.Contains(t, out, "✓ Cleared", "expected a successful cleared message")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.SourceURL, "SourceURL not cleared")
	assert.Empty(t, cfg.TargetURL, "TargetURL not cleared")

	store2, err := creds.NewStore()
	require.NoError(t, err)
	v, _ := store2.Get(creds.SourceToken)
	assert.Empty(t, v, "source token not cleared")
	v, _ = store2.Get(creds.TargetToken)
	assert.Empty(t, v, "target token not cleared")
}

func TestConfigureShowAndResetMutuallyExclusive(t *testing.T) {
	seedFileStore(t)
	root := NewRootCmd("test")
	root.SetArgs([]string{"configure", "--show", "--reset"})
	err := root.Execute()
	require.Error(t, err, "expected an error when --show and --reset are combined")
	assert.Contains(t, err.Error(), "show", "error should name the conflicting flags")
	assert.Contains(t, err.Error(), "reset", "error should name the conflicting flags")
}

func TestValidateURL(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		for _, s := range []string{
			"https://ghes.example.com",
			"http://localhost:8080",
			"https://api.tenant.ghe.com/",
		} {
			t.Run(s, func(t *testing.T) {
				assert.NoError(t, validateURL(s))
			})
		}
	})

	t.Run("invalid", func(t *testing.T) {
		for _, s := range []string{"", "   ", "ftp://host", "notaurl", "https://"} {
			t.Run(s, func(t *testing.T) {
				require.Error(t, validateURL(s))
			})
		}
	})
}

func TestNormalizeTargetAPIURL(t *testing.T) {
	t.Run("prefixes the hostname and preserves URL components", func(t *testing.T) {
		got := normalizeTargetAPIURL(" https://user@staffship.blabla.com:8443/base?mode=test#fragment ")

		assert.Equal(t, "https://user@api.staffship.blabla.com:8443/base?mode=test#fragment", got)
	})

	t.Run("preserves an existing API label case insensitively", func(t *testing.T) {
		assert.Equal(t, "https://API.staffship.blabla.com", normalizeTargetAPIURL("https://API.staffship.blabla.com"))
	})

	t.Run("prefixes a hostname that only contains api elsewhere", func(t *testing.T) {
		assert.Equal(t, "https://api.staffship-api.blabla.com", normalizeTargetAPIURL("https://staffship-api.blabla.com"))
	})

	t.Run("preserves local development endpoints", func(t *testing.T) {
		for _, raw := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
			assert.Equal(t, raw, normalizeTargetAPIURL(raw))
		}
	})
}
