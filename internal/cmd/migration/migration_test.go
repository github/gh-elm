package migration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	t.Run("creates a migration and prints the response JSON", func(t *testing.T) {
		var gotPath, gotMethod string
		var gotBody elmapiCreateBody
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotMethod = r.URL.Path, r.Method
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migration_id":"mig-1","expires_at":null}`))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		out := run(t, "create", "--source-org", "acme", "--source-repo", "web",
			"--target-org", "acme-cloud", "--target-repo", "web",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, http.MethodPost, gotMethod)
		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations"), "path suffix: %q", gotPath)
		// The target endpoint is derived from GH_TARGET_HOST (API-defect workaround).
		assert.Equal(t, "https://api.example.ghe.com", gotBody.TargetAPIEndpoint)
		// pat_name is stubbed with a sentinel (API-defect workaround).
		assert.Equal(t, "BOGON", gotBody.PATName)
		assert.Equal(t, "web", gotBody.SourceRepositoryName)
		assert.Equal(t, "internal", gotBody.TargetVisibility)
		assert.Contains(t, out, `"migration_id": "mig-1"`, "output missing migration_id")
	})

	t.Run("create --start posts start and reports success", func(t *testing.T) {
		var startCalled bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/start") {
				startCalled = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migration_id":"mig-2","expires_at":null}`))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		out := run(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--start", "--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, startCalled, "start endpoint was not called")
		assert.Contains(t, out, "created and started")
	})

	t.Run("rejects public visibility", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r", "--target-visibility", "public",
			"--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "private or internal")
	})

	t.Run("--watch requires --start", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--watch", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--watch requires --start")
	})

	t.Run("requires the core flags", func(t *testing.T) {
		err := runErr(t, "create", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("errors when no target host is configured (API-defect workaround)", func(t *testing.T) {
		t.Setenv("GH_TARGET_HOST", "")
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a target endpoint")
	})
}

func TestStart(t *testing.T) {
	t.Run("posts to the start endpoint", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		out := run(t, "start", "--migration-id", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations/mig-1/start"), "path = %q", gotPath)
		assert.Contains(t, out, "started")
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runErr(t, "start", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration-id")
	})
}

func TestStatus(t *testing.T) {
	t.Run("prints the raw API JSON", func(t *testing.T) {
		const respBody = `{"migration":{"migration_id":"mig-1","status":"in_progress"},"target_state":null,"combined_state":null,"messages":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations/mig-1"), "path = %q", r.URL.Path)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "status", "--migration-id", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, respBody+"\n", out) //nolint:testifylint // encoded-compare
	})
}

func TestList(t *testing.T) {
	t.Run("passes filters and prints raw JSON", func(t *testing.T) {
		var gotQuery string
		const respBody = `{"migrations":[],"total_count":0}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "list", "--status", "in_progress", "--page-size", "25",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.Contains(t, gotQuery, "status=in_progress")
		assert.Contains(t, gotQuery, "page_size=25")
		assert.Equal(t, respBody+"\n", out) //nolint:testifylint // encoded-compare
	})

	t.Run("rejects an invalid status", func(t *testing.T) {
		err := runErr(t, "list", "--status", "bogus",
			"--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --status")
	})
}

func TestActions(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantPath string
		respCode int
		respBody string
		wantOut  string
	}{
		{"cancel", []string{"cancel", "--migration-id", "m"}, "/enterprise/live-migrations/m/cancel", http.StatusNoContent, "", "cancelled"},
		{"cutover", []string{"cutover-to-destination", "--migration-id", "m"}, "/enterprise/live-migrations/m/cutover", http.StatusNoContent, "", "Cutover initiated"},
		{"pause", []string{"pause", "--migration-id", "m"}, "/enterprise/live-migrations/m/pause", http.StatusNoContent, "", "paused"},
		{"resume", []string{"resume", "--migration-id", "m"}, "/enterprise/live-migrations/m/resume", http.StatusNoContent, "", "resumed"},
		{"revert-cutover", []string{"revert-cutover", "--migration-id", "m"}, "/enterprise/live-migrations/m/revert-cutover", http.StatusOK, `{"success":true,"unarchived_source_repository":true,"in_progress_cutover_terminated":false,"in_progress_migration_terminated":false}`, `"success": true`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				w.WriteHeader(tc.respCode)
				if tc.respBody != "" {
					_, _ = w.Write([]byte(tc.respBody))
				}
			}))
			defer srv.Close()

			args := make([]string, len(tc.args), len(tc.args)+4)
			copy(args, tc.args)
			args = append(args, "--source-url", srv.URL, "--source-token", "tok")
			out := run(t, args...)

			assert.Equal(t, http.MethodPost, gotMethod)
			assert.True(t, strings.HasSuffix(gotPath, tc.wantPath), "path = %q, want suffix %q", gotPath, tc.wantPath)
			assert.Contains(t, out, tc.wantOut)
		})
	}
}

func TestCutover(t *testing.T) {
	t.Run("sends force in the body", func(t *testing.T) {
		var gotForce bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Force bool `json:"force"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotForce = body.Force
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		run(t, "cutover-to-destination", "--migration-id", "m", "--force",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, gotForce, "expected force=true in cutover body")
	})
}

func TestCutoverStatus(t *testing.T) {
	t.Run("renders cutover readiness from combined state", func(t *testing.T) {
		const respBody = `{"migration":null,"target_state":null,"combined_state":{"status":"backfilling","display_message":"Backfill in progress","ready_for_cutover":false,"cutover_blockers":["backfill incomplete"],"repositories":[{"repository_nwo":"acme/web","phase":"backfill","display_status":"In progress"}]},"messages":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "cutover-status", "--migration-id", "m",
			"--source-url", srv.URL, "--source-token", "tok")

		for _, want := range []string{"Ready for cutover: false", "backfill incomplete", "acme/web"} {
			assert.Contains(t, out, want)
		}
	})
}

func TestAuthErrorAnnotation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	err := runErr(t, "status", "--migration-id", "m",
		"--source-url", srv.URL, "--source-token", "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func TestLookupTargetID(t *testing.T) {
	const withTargetID = `{"migration":{"migration_id":"mig-1","target_migration_id":12345},"target_state":null,"combined_state":null,"messages":[]}`

	t.Run("prints the target migration ID (human)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations/mig-1") {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(withTargetID))
		}))
		defer srv.Close()

		out := run(t, "lookup-target-id", "--migration-id", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		if !strings.Contains(out, "Target migration ID: 12345") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("--json emits a machine-readable object", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(withTargetID))
		}))
		defer srv.Close()

		out := run(t, "lookup-target-id", "--migration-id", "mig-1", "--json",
			"--source-url", srv.URL, "--source-token", "tok")

		var got targetIDView
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("output is not valid JSON: %v\n%s", err, out)
		}
		if got.MigrationID != "mig-1" || got.TargetMigrationID != 12345 {
			t.Errorf("got = %+v", got)
		}
	})

	t.Run("errors on a migration ID that does not exist", func(t *testing.T) {
		// A mistyped / unknown migration ID returns 404 from GHES. The command
		// must surface that as an error rather than printing a target ID, so a
		// nonexistent migration is never mistaken for a successful lookup.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}))
		defer srv.Close()

		out, err := exec(t, "lookup-target-id", "--migration-id", "does-not-exist",
			"--source-url", srv.URL, "--source-token", "tok")
		if err == nil {
			t.Fatalf("expected an error for a missing migration, got output:\n%s", out)
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected a 404 error, got %v", err)
		}
		if strings.Contains(out, "Target migration ID") {
			t.Errorf("missing migration must not print a target ID:\n%s", out)
		}
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runErr(t, "lookup-target-id", "--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})
}

// elmapiCreateBody mirrors the create request body for assertions.
type elmapiCreateBody struct {
	SourceRepositoryName string `json:"source_repository_name"`
	TargetAPIEndpoint    string `json:"target_api_endpoint"`
	PATName              string `json:"pat_name"`
	TargetVisibility     string `json:"target_visibility"`
}

// run executes a `migration` subcommand and returns output, failing on error.
func run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec(t, args...)
	require.NoErrorf(t, err, "migration %v output:\n%s", args, out)
	return out
}

// runErr executes a `migration` subcommand and returns the error (if any).
func runErr(t *testing.T, args ...string) error {
	_, err := exec(t, args...)
	return err
}

func exec(t *testing.T, args ...string) (string, error) {
	t.Setenv("GH_ELM_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_ELM_CREDENTIAL_STORE", "file")

	cmd := NewCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	err := cmd.Execute()
	return buf.String(), err
}
