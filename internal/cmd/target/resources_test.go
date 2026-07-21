package target

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

		if !strings.Contains(out, "Resource ID: n1") {
			t.Errorf("missing resource line in output:\n%s", out)
		}
		if !strings.Contains(out, "Found 2 resources.") {
			t.Errorf("missing summary in output:\n%s", out)
		}
		if len(origins) != 2 {
			t.Errorf("expected 2 origin queries (backfill+live_update), got %v", origins)
		}
	})

	t.Run("emits newline-delimited JSON with --json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"n1"}],"after":""}`))
		}))
		defer srv.Close()

		out := runResources(t, "--migration-id", "1", "--origin", "backfill", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		if !strings.Contains(out, `"id":"n1"`) {
			t.Errorf("expected JSON resource, got:\n%s", out)
		}
		if strings.Contains(out, "Found ") {
			t.Errorf("JSON output should not include the human summary:\n%s", out)
		}
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

		if !strings.Contains(out, `"correlationId":"abc-123"`) {
			t.Errorf("expected unknown field preserved, got:\n%s", out)
		}
		for _, absent := range []string{"createdAt", "updatedAt", "origin", "state"} {
			if strings.Contains(out, absent) {
				t.Errorf("re-marshaled zero field %q leaked into output:\n%s", absent, out)
			}
		}
	})

	t.Run("respects --max-results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"nodes":[{"id":"a"},{"id":"b"},{"id":"c"}],"after":""}`))
		}))
		defer srv.Close()

		out := runResources(t, "--migration-id", "1", "--origin", "backfill", "--max-results", "2",
			"--target-url", srv.URL, "--target-token", "tok")

		if strings.Count(out, "Resource ID:") != 2 {
			t.Errorf("expected 2 resources, got:\n%s", out)
		}
	})

	t.Run("rejects an invalid state", func(t *testing.T) {
		err := runResourcesErr(t, "--migration-id", "1", "--state", "bogus",
			"--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "invalid --state") {
			t.Fatalf("expected invalid state error, got %v", err)
		}
	})

	t.Run("surfaces an actionable auth error on 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := runResourcesErr(t, "--migration-id", "1", "--origin", "backfill",
			"--target-url", srv.URL, "--target-token", "bad")
		if err == nil {
			t.Fatal("expected an error on 401")
		}
		msg := err.Error()
		for _, want := range []string{"authentication failed", "401", "MIGRATION_TARGET_URL", "MIGRATION_TARGET_TOKEN"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q missing %q", msg, want)
			}
		}
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runResourcesErr(t, "--target-url", "https://x", "--target-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "migration-id") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})
}

// runResources executes `target resources` with the given args, isolating
// config/creds to a temp dir, and returns combined output. It fails on error.
func runResources(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execResources(t, args...)
	if err != nil {
		t.Fatalf("target resources %v: %v\noutput:\n%s", args, err, out)
	}
	return out
}

// runResourcesErr executes `target resources` and returns the error (if any).
func runResourcesErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := execResources(t, args...)
	return err
}

func execResources(t *testing.T, args ...string) (string, error) {
	t.Helper()
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
