package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigCheckResolverFailure(t *testing.T) {
	assertBothEndpointsReported := func(t *testing.T) {
		t.Helper()

		output, err := execConfigure("check")

		require.Error(t, err)
		for _, endpoint := range []string{"Source", "Target"} {
			for _, check := range []string{"network", "service", "authentication"} {
				assert.Contains(t, output, fmt.Sprintf("[FAIL] %s %s: not checked because configuration could not be read", endpoint, check))
			}
		}
	}

	t.Run("malformed config reports both endpoints", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("GH_ELM_CONFIG_DIR", configDir)
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{"), 0o600))

		assertBothEndpointsReported(t)
	})

	t.Run("invalid credential store reports both endpoints", func(t *testing.T) {
		t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "invalid")

		assertBothEndpointsReported(t)
	})
}
