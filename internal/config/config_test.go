package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	// A missing file loads as an empty config.
	got, err := Load()
	require.NoError(t, err, "Load on empty dir")
	assert.Empty(t, got.SourceURL)
	assert.Empty(t, got.TargetURL)

	want := &Config{SourceURL: "https://ghes.example.com", TargetURL: "https://api.tenant.ghe.com"}
	require.NoError(t, want.Save(), "Save")

	got, err = Load()
	require.NoError(t, err, "Load after Save")
	assert.Equal(t, *want, *got)
}

func TestSavePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GH_ELM_CONFIG_DIR", dir)

	require.NoError(t, (&Config{SourceURL: "https://x"}).Save())

	info, err := os.Stat(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "config file perm")
}

func TestDirPrecedence(t *testing.T) {
	t.Setenv("GH_ELM_CONFIG_DIR", "/tmp/elm-explicit")
	t.Setenv("GH_CONFIG_DIR", "/tmp/gh")
	d, err := Dir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/elm-explicit", d, "GH_ELM_CONFIG_DIR should win")

	t.Setenv("GH_ELM_CONFIG_DIR", "")
	d, err = Dir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/gh/gh-elm", d, "GH_CONFIG_DIR should be used")
}
