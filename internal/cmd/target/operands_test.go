package target

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSourceMigrationUUID = "897930cf-51cb-4e2d-9806-6357a6e66b55"

func resolveTargetMigrationIDForTest(
	t *testing.T,
	positional string,
	flags ...string,
) (int64, error) {
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	var options targetMigrationIDOptions
	options.addFlags(cmd)
	require.NoError(t, cmd.ParseFlags(flags))
	return options.resolve(cmd, positional)
}

func TestResolveTargetMigrationID(t *testing.T) {
	t.Run("returns a numeric target ID without source configuration", func(t *testing.T) {
		t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

		got, err := resolveTargetMigrationIDForTest(t, "42")

		require.NoError(t, err)
		assert.Equal(t, int64(42), got)
	})

	t.Run("resolves a source UUID through the source API", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/enterprise/live-migrations/"+testSourceMigrationUUID, r.URL.Path)
			assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"migration":{"target_migration_id":84}}`))
		}))
		defer srv.Close()

		got, err := resolveTargetMigrationIDForTest(
			t,
			testSourceMigrationUUID,
			"--source-url", srv.URL,
			"--source-token", "tok",
		)

		require.NoError(t, err)
		assert.Equal(t, int64(84), got)
	})

	t.Run("accepts a source UUID from the compatibility flag", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"target_migration_id":85}}`))
		}))
		defer srv.Close()

		got, err := resolveTargetMigrationIDForTest(
			t,
			"",
			"--migration-id", testSourceMigrationUUID,
			"--source-url", srv.URL,
			"--source-token", "tok",
		)

		require.NoError(t, err)
		assert.Equal(t, int64(85), got)
	})

	t.Run("requires source configuration only for UUIDs", func(t *testing.T) {
		t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
		t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

		_, err := resolveTargetMigrationIDForTest(t, testSourceMigrationUUID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a source URL")
	})

	t.Run("rejects a migration without a target ID", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"target_migration_id":0}}`))
		}))
		defer srv.Close()

		_, err := resolveTargetMigrationIDForTest(
			t,
			testSourceMigrationUUID,
			"--source-url", srv.URL,
			"--source-token", "tok",
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not have a target migration ID yet")
	})

	t.Run("rejects malformed identifiers", func(t *testing.T) {
		for _, value := range []string{"0", "-1", "not-a-uuid", "897930cf-51cb-4e2d-9806"} {
			_, err := resolveTargetMigrationIDForTest(t, value)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "positive integer or canonical UUID")
		}
	})
}
