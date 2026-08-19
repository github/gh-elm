package cmd

import (
	"bytes"
	"errors"
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

// execConfigure runs `gh elm configure <args>` through the root command and
// returns combined output. Callers set GH_ELM_CONFIG_DIR / GH_ELM_CREDENTIAL_STORE
// (and seed state) beforehand so the run is isolated to a temp dir.
func execConfigure(args ...string) (string, error) {
	root := NewRootCmd("test")
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"configure"}, args...))
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

	out, err := execConfigure("--show")
	require.NoError(t, err, "configure --show")
	assert.Contains(t, out, "https://ghes.example.com", "missing source url in output")
	assert.Contains(t, out, "set (hidden)", "seeded source token should read as set")
	assert.Contains(t, out, "not set", "unset target token should read as not set")
}

func TestConfigureAlias(t *testing.T) {
	t.Run("executes show", func(t *testing.T) {
		seedFileStore(t)

		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs([]string{"config", "--show"})

		require.NoError(t, root.Execute())
		assert.Contains(t, output.String(), "Source (GHES)")
	})

	t.Run("is hidden from configure help", func(t *testing.T) {
		root := NewRootCmd("test")
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetArgs([]string{"configure", "--help"})

		require.NoError(t, root.Execute())
		assert.NotContains(t, output.String(), "ALIASES")
		assert.NotContains(t, output.String(), "configure, config")
	})
}

func TestConfigureReset(t *testing.T) {
	seedFileStore(t)
	require.NoError(t, (&config.Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}).Save(), "seed config")
	store, err := creds.NewStore()
	require.NoError(t, err, "NewStore")
	require.NoError(t, store.Set(creds.SourceToken, "ghp_source"))
	require.NoError(t, store.Set(creds.TargetToken, "ghp_target"))

	out, err := execConfigure("--reset")
	require.NoError(t, err, "configure --reset")
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
	_, err := execConfigure("--show", "--reset")
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
