package target

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResources(t *testing.T) {
	t.Run("lists resources across both origins in human-readable form", func(t *testing.T) {
		var origins []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origins = append(origins, r.URL.Query().Get("origin"))
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1","type":"issue","origin":"NODE_ORIGIN_BACKFILL","state":"NODE_STATE_PENDING"}],"after":""}`))
		}))
		defer srv.Close()

		out := runResources(t, "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Contains(t, out, "Resource ID: n1", "missing resource line")
		assert.Contains(t, out, "Found 2 resources.", "missing summary")
		assert.Len(t, origins, 2, "expected 2 origin queries (backfill+live_update)")
	})

	t.Run("emits newline-delimited JSON with --json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"}],"after":""}`))
		}))
		defer srv.Close()

		out := runResources(t, "--migration-id", "1", "--origin", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Contains(t, out, `"id":"n1"`, "expected JSON resource")
		assert.NotContains(t, out, "Found ", "JSON output should not include the human summary")
	})

	t.Run("--json echoes the raw API JSON verbatim", func(t *testing.T) {
		// The API response carries a field Node does not model (correlationId)
		// and omits others (no timestamps). The --json output must preserve the
		// unknown field and must not inject zero-valued/absent fields.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1","type":"issue","correlationId":"abc-123"}],"after":""}`))
		}))
		defer srv.Close()

		out := strings.TrimSpace(runResources(t, "--migration-id", "1", "--origin", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok"))

		assert.Contains(t, out, `"correlationId":"abc-123"`, "expected unknown field preserved")
		for _, absent := range []string{"createdAt", "updatedAt", "origin", "state"} {
			assert.NotContains(t, out, absent, "re-marshaled zero field leaked into output")
		}
	})

	t.Run("respects --max-results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"a"},{"id":"b"},{"id":"c"}],"after":""}`))
		}))
		defer srv.Close()

		out := runResources(t, "--migration-id", "1", "--origin", "backfill", "--max-results", "2",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, 2, strings.Count(out, "Resource ID:"), "expected 2 resources")
	})

	t.Run("rejects an invalid state", func(t *testing.T) {
		err := runResourcesErr(t, "--migration-id", "1", "--state", "bogus",
			"--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --state")
	})

	t.Run("surfaces an actionable auth error on 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := runResourcesErr(t, "--migration-id", "1", "--origin", "backfill",
			"--target-url", srv.URL, "--target-token", "bad")
		require.Error(t, err, "expected an error on 401")
		msg := err.Error()
		for _, want := range []string{"authentication failed", "401", "GH_TARGET_HOST", "GH_TARGET_TOKEN"} {
			assert.Contains(t, msg, want)
		}
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runResourcesErr(t, "--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration-id")
	})
}

// runResources executes `target resources` with the given args, isolating
// config/creds to a temp dir, and returns combined output. It fails on error.
func runResources(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execResources(t, args...)
	require.NoErrorf(t, err, "target resources %v output:\n%s", args, out)
	return out
}

// runResourcesErr executes `target resources` and returns the error (if any).
func runResourcesErr(t *testing.T, args ...string) error {
	_, err := execResources(t, args...)
	return err
}

func execResources(t *testing.T, args ...string) (string, error) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	cmd := NewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(append([]string{"resources"}, args...))

	err := cmd.Execute()
	return buf.String(), err
}
