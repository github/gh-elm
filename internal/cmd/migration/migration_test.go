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

	"github.com/github/gh-elm/internal/elmapi"
)

func TestCreate(t *testing.T) {
	t.Run("creates a migration from positional repositories and prints human-readable output", func(t *testing.T) {
		var gotPath, gotMethod string
		var gotBody elmapiCreateBody
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				assert.Equal(t, elmapi.StatusCreated, r.URL.Query().Get("status"))
				assert.Equal(t, "100", r.URL.Query().Get("page_size"))
				_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
				return
			}
			gotPath, gotMethod = r.URL.Path, r.Method
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migration_id":"mig-1","expires_at":null}`))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		out := run(t, "create", "acme/web", "acme-cloud/web",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, http.MethodPost, gotMethod)
		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations"), "path suffix: %q", gotPath)
		// The target endpoint is derived from GH_TARGET_HOST (API-defect workaround).
		assert.Equal(t, "https://api.example.ghe.com", gotBody.TargetAPIEndpoint)
		// pat_name is stubbed with a sentinel (API-defect workaround).
		assert.Equal(t, "BOGON", gotBody.PATName)
		assert.Equal(t, "acme", gotBody.SourceOrganizationLogin)
		assert.Equal(t, "web", gotBody.SourceRepositoryName)
		assert.Equal(t, "acme-cloud", gotBody.TargetOrganizationLogin)
		assert.Equal(t, "web", gotBody.TargetRepositoryName)
		assert.Equal(t, "internal", gotBody.TargetVisibility)
		assert.Contains(t, out, "✓ Migration successfully created")
		assert.Contains(t, out, "Migration ID")
		assert.Contains(t, out, "mig-1")
	})

	t.Run("--json preserves and formats the create response", func(t *testing.T) {
		const respBody = `{"migration_id":"mig-1","expires_at":null,"future_field":"preserved"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		out := run(t, "create", "--source-org", "acme", "--source-repo", "web",
			"--target-org", "acme-cloud", "--target-repo", "web", "--json",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.JSONEq(t, respBody, out)
		assert.Contains(t, out, "\n  \"migration_id\":")
	})

	t.Run("create --start posts start and reports success", func(t *testing.T) {
		var startCalled bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
				return
			}
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
		assert.Contains(t, out, "✓ Migration mig-2 created and started.")
	})

	t.Run("rejects mixed positional and flag repositories", func(t *testing.T) {
		err := runErr(t, "create", "a/r", "b/r", "--source-org", "a",
			"--source-url", "https://x", "--source-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be combined")
	})

	t.Run("rejects malformed positional repositories", func(t *testing.T) {
		err := runErr(t, "create", "a/r/extra", "b/r",
			"--source-url", "https://x", "--source-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid source repository")
		assert.Contains(t, err.Error(), "exactly one slash")
	})

	t.Run("rejects a partial repository flag set", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--source-url", "https://x", "--source-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "all four repository flags")
	})

	t.Run("rejects a duplicate created migration case-insensitively", func(t *testing.T) {
		var createCalled bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				createCalled = true
				w.WriteHeader(http.StatusCreated)
				return
			}
			_, _ = w.Write([]byte(`{"migrations":[{
				"migration_id":"existing-id",
				"source_organization_login":"ACME",
				"source_repository_name":"WEB",
				"target_organization_login":"ACME-CLOUD",
				"target_repository_name":"WEB"
			}],"total_count":1}`))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		err := runErr(t, "create", "acme/web", "acme-cloud/web",
			"--source-url", srv.URL, "--source-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "created migration already exists")
		assert.Contains(t, err.Error(), "existing-id")
		assert.False(t, createCalled)
	})

	t.Run("checks every created migration page", func(t *testing.T) {
		var cursors []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cursors = append(cursors, r.URL.Query().Get("after"))
			if r.URL.Query().Get("after") == "" {
				_, _ = w.Write([]byte(`{"migrations":[{
					"migration_id":"other-id",
					"source_organization_login":"other",
					"source_repository_name":"repo",
					"target_organization_login":"elsewhere",
					"target_repository_name":"repo"
				}],"total_count":2,"next_cursor":"page-2"}`))
				return
			}
			_, _ = w.Write([]byte(`{"migrations":[{
				"migration_id":"existing-id",
				"source_organization_login":"acme",
				"source_repository_name":"web",
				"target_organization_login":"acme-cloud",
				"target_repository_name":"web"
			}],"total_count":2}`))
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		err := runErr(t, "create", "acme/web", "acme-cloud/web",
			"--source-url", srv.URL, "--source-token", "tok")

		require.Error(t, err)
		assert.Equal(t, []string{"", "page-2"}, cursors)
		assert.Contains(t, err.Error(), "existing-id")
	})

	t.Run("surfaces duplicate preflight failures", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		t.Setenv("GH_TARGET_HOST", "api.example.ghe.com")
		err := runErr(t, "create", "acme/web", "acme-cloud/web",
			"--source-url", srv.URL, "--source-token", "tok")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "checking for an existing migration")
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

	t.Run("--json cannot be combined with --watch", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--start", "--watch", "--json", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--json cannot be used with --watch")
	})

	t.Run("requires the core flags", func(t *testing.T) {
		err := runErr(t, "create", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires")
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

		out := run(t, "start", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations/mig-1/start"), "path = %q", gotPath)
		assert.Contains(t, out, "✓ Migration mig-1 started.")
	})

	t.Run("requires a migration ID", func(t *testing.T) {
		err := runErr(t, "start", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
	})
}

func TestStatus(t *testing.T) {
	t.Run("prints human-readable status", func(t *testing.T) {
		const respBody = `{"migration":{"migration_id":"mig-1","status":"in_progress"},"target_state":null,"combined_state":null,"messages":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations/mig-1"), "path = %q", r.URL.Path)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "status", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		for _, want := range []string{"Migration", "Migration ID", "mig-1", "In progress"} {
			assert.Contains(t, out, want)
		}
	})

	t.Run("--json preserves the raw status response", func(t *testing.T) {
		const respBody = `{"migration":{"migration_id":"mig-1"},"future_field":{"value":1}}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "status", "--migration-id", "mig-1", "--json",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.JSONEq(t, respBody, out)
		assert.Contains(t, out, "\n  \"migration\":")
	})
}

func TestList(t *testing.T) {
	t.Run("passes filters and prints human-readable output", func(t *testing.T) {
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
		assert.Contains(t, out, "No migrations available.")
		assert.Contains(t, out, "Create one with `gh elm migration create --help`.")
		assert.NotContains(t, out, "Showing 0")
	})

	t.Run("--json preserves the raw list response", func(t *testing.T) {
		const respBody = `{"migrations":[],"total_count":0,"future_field":"preserved"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "list", "--json",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.JSONEq(t, respBody, out)
		assert.Contains(t, out, "\n  \"migrations\":")
	})

	t.Run("bare list falls back to created migrations", func(t *testing.T) {
		const createdBody = `{"migrations":[{"migration_id":"created-id","status":"created"}],"total_count":1}`
		var statuses []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status := r.URL.Query().Get("status")
			statuses = append(statuses, status)
			if status == "" {
				_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
				return
			}
			_, _ = w.Write([]byte(createdBody))
		}))
		defer srv.Close()

		out := run(t, "list", "--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, []string{"", elmapi.StatusCreated}, statuses)
		assert.Contains(t, out, "Migrations (1)")
		assert.Contains(t, out, "created-id")
	})

	t.Run("does not fall back when page size is explicit", func(t *testing.T) {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
		}))
		defer srv.Close()

		run(t, "list", "--page-size", "25", "--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, 1, requests)
	})

	t.Run("does not fall back when a cursor is explicit", func(t *testing.T) {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"migrations":[],"total_count":0}`))
		}))
		defer srv.Close()

		run(t, "list", "--after", "cursor", "--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, 1, requests)
	})

	t.Run("does not fall back when the default response is non-empty", func(t *testing.T) {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"migrations":[{"migration_id":"active-id","status":"in_progress"}],"total_count":1}`))
		}))
		defer srv.Close()

		out := run(t, "list", "--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, 1, requests)
		assert.Contains(t, out, "active-id")
	})

	t.Run("does not fall back when migrations are returned despite a zero total count", func(t *testing.T) {
		var requests int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			_, _ = w.Write([]byte(`{"migrations":[{"migration_id":"active-id","status":"in_progress"}],"total_count":0}`))
		}))
		defer srv.Close()

		out := run(t, "list", "--source-url", srv.URL, "--source-token", "tok")

		assert.Equal(t, 1, requests)
		assert.Contains(t, out, "active-id")
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
		{"cancel", []string{"cancel", "m"}, "/enterprise/live-migrations/m/cancel", http.StatusNoContent, "", "cancelled"},
		{"cutover", []string{"cutover", "m"}, "/enterprise/live-migrations/m/cutover", http.StatusNoContent, "", "Cutover initiated"},
		{"pause", []string{"pause", "m"}, "/enterprise/live-migrations/m/pause", http.StatusNoContent, "", "paused"},
		{"resume", []string{"resume", "m"}, "/enterprise/live-migrations/m/resume", http.StatusNoContent, "", "resumed"},
		{"cutover revert", []string{"cutover", "revert", "m"}, "/enterprise/live-migrations/m/revert-cutover", http.StatusOK, `{"success":true,"unarchived_source_repository":true,"in_progress_cutover_terminated":false,"in_progress_migration_terminated":false}`, "Cutover reverted"},
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
			assert.Contains(t, out, "✓")
			assert.Contains(t, out, tc.wantOut)
		})
	}
}

func TestCancel(t *testing.T) {
	t.Run("accepts the kill alias", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		run(t, "kill", "mig-1", "--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations/mig-1/cancel"))
	})

	t.Run("accepts the migration ID flag", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		run(t, "cancel", "--migration-id", "mig-1", "--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations/mig-1/cancel"))
	})

	t.Run("rejects positional and flag IDs together", func(t *testing.T) {
		err := runErr(t, "cancel", "mig-1", "--migration-id", "mig-2")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be combined")
	})

	t.Run("requires a migration ID", func(t *testing.T) {
		err := runErr(t, "cancel")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
	})

	t.Run("rejects extra migration IDs", func(t *testing.T) {
		err := runErr(t, "cancel", "mig-1", "mig-2")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 1 arg")
	})

	t.Run("help hides the alias", func(t *testing.T) {
		out, err := exec(t, "cancel", "--help")

		require.NoError(t, err)
		assert.NotContains(t, out, "ALIASES")
		assert.NotContains(t, out, "kill")
	})
}

func TestRevertCutoverJSON(t *testing.T) {
	t.Run("preserves the raw response", func(t *testing.T) {
		const respBody = `{"success":true,"unarchived_source_repository":true,"future_field":"preserved"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "cutover", "revert", "--migration-id", "m", "--json",
			"--source-url", srv.URL, "--source-token", "tok")
		assert.JSONEq(t, respBody, out)
		assert.Contains(t, out, "\n  \"success\":")
	})
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

		run(t, "cutover", "m", "--force",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, gotForce, "expected force=true in cutover body")
	})

	t.Run("accepts the old command name as an alias", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		run(t, "cutover-to-destination", "m",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.True(t, strings.HasSuffix(gotPath, "/enterprise/live-migrations/m/cutover"))
	})

	t.Run("hides the compatibility alias from help", func(t *testing.T) {
		out, err := exec(t, "cutover", "--help")

		require.NoError(t, err)
		assert.NotContains(t, out, "ALIASES")
		assert.NotContains(t, out, "cutover-to-destination")
		assert.Contains(t, out, "migration cutover [MIGRATION-ID] [flags]")
		assert.Contains(t, out, "migration cutover [command]")
	})
}

func TestCutoverStatus(t *testing.T) {
	t.Run("renders cutover readiness from combined state", func(t *testing.T) {
		const respBody = `{"migration":null,"target_state":null,"combined_state":{"status":"backfilling","display_message":"Backfill in progress","ready_for_cutover":false,"cutover_blockers":["backfill incomplete"],"repositories":[{"repository_nwo":"acme/web","phase":"backfill","display_status":"In progress"}]},"messages":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "cutover", "status", "m",
			"--source-url", srv.URL, "--source-token", "tok")

		for _, want := range []string{"○ Not ready for cutover", "Backfill in progress", "backfill incomplete", "acme/web · Backfill · In progress"} {
			assert.Contains(t, out, want)
		}
		assert.NotContains(t, out, "Ready for cutover: false")
	})

	t.Run("accepts the legacy flat command", func(t *testing.T) {
		err := runErr(t, "cutover-status")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
		assert.NotContains(t, err.Error(), "unknown command")
	})
}

func TestRevertCutoverCompatibility(t *testing.T) {
	err := runErr(t, "revert-cutover")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration ID required")
	assert.NotContains(t, err.Error(), "unknown command")
}

func TestSourceErrorAnnotation(t *testing.T) {
	t.Run("annotates an authentication failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		}))
		defer srv.Close()

		err := runErr(t, "status", "--migration-id", "m",
			"--source-url", srv.URL, "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})

	t.Run("explains unavailable ELM when authentication succeeds", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "list", "--source-url", srv.URL, "--source-token", "tok")
		require.EqualError(t, err, elmUnavailableMessage)
	})

	t.Run("explains when the source GHES version is too old", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("X-GitHub-Enterprise-Version", "3.18.10")
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "list", "--source-url", srv.URL, "--source-token", "tok")
		require.EqualError(t, err, "source GHES version 3.18.10 does not support ELM; upgrade to GHES 3.18.11 or later")
	})

	t.Run("keeps the generic error for a sufficient source GHES version", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("X-GitHub-Enterprise-Version", "3.18.11")
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "list", "--source-url", srv.URL, "--source-token", "tok")
		require.EqualError(t, err, elmUnavailableMessage)
	})

	t.Run("keeps the generic error for an unrecognized source version", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("X-GitHub-Enterprise-Version", "unknown")
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "list", "--source-url", srv.URL, "--source-token", "tok")
		require.EqualError(t, err, elmUnavailableMessage)
	})

	t.Run("reports failed authentication behind an ELM 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/user") {
				http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
				return
			}
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "list", "--source-url", srv.URL, "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "authentication failed")
	})

	t.Run("preserves a missing migration error when ELM is available", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations") {
				_, _ = w.Write([]byte(`{"migrations":[],"total_count":0,"next_cursor":""}`))
				return
			}
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()

		err := runErr(t, "status", "--migration-id", "does-not-exist",
			"--source-url", srv.URL, "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
		assert.NotContains(t, err.Error(), elmUnavailableMessage)
	})
}

func TestMinimumELMVersion(t *testing.T) {
	t.Run("rejects a release before ELM support", func(t *testing.T) {
		minimum, unsupported := minimumELMVersion("3.16.9")
		assert.True(t, unsupported)
		assert.Equal(t, "3.17.17", minimum)
	})

	t.Run("enforces the 3.17 release floor", func(t *testing.T) {
		assertReleaseFloor(t, "3.17.16", "3.17.17")
	})

	t.Run("enforces the 3.18 release floor", func(t *testing.T) {
		assertReleaseFloor(t, "3.18.10", "3.18.11")
	})

	t.Run("enforces the 3.19 release floor", func(t *testing.T) {
		assertReleaseFloor(t, "3.19.7", "3.19.8")
	})

	t.Run("enforces the 3.20 release floor", func(t *testing.T) {
		assertReleaseFloor(t, "3.20.3", "3.20.4")
	})

	t.Run("enforces the 3.21 release floor", func(t *testing.T) {
		assertReleaseFloor(t, "3.21.1", "3.21.2")
	})

	t.Run("accepts a newer release line", func(t *testing.T) {
		minimum, unsupported := minimumELMVersion("3.22.0")
		assert.False(t, unsupported)
		assert.Empty(t, minimum)
	})

	t.Run("accepts a version suffix", func(t *testing.T) {
		minimum, unsupported := minimumELMVersion("3.19.7-rc1")
		assert.True(t, unsupported)
		assert.Equal(t, "3.19.8", minimum)
	})

	t.Run("leaves a malformed version inconclusive", func(t *testing.T) {
		minimum, unsupported := minimumELMVersion("not-a-version")
		assert.False(t, unsupported)
		assert.Empty(t, minimum)
	})
}

func assertReleaseFloor(t *testing.T, below, floor string) {
	t.Helper()

	minimum, unsupported := minimumELMVersion(below)
	assert.True(t, unsupported, "version %s", below)
	assert.Equal(t, floor, minimum, "version %s", below)

	minimum, unsupported = minimumELMVersion(floor)
	assert.False(t, unsupported, "version %s", floor)
	assert.Empty(t, minimum, "version %s", floor)
}

func TestTargetID(t *testing.T) {
	const withTargetID = `{"migration":{"migration_id":"mig-1","target_migration_id":12345},"target_state":null,"combined_state":null,"messages":[]}`

	t.Run("prints the target migration ID (human)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.True(t, strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations/mig-1"), "path = %q", r.URL.Path)
			_, _ = w.Write([]byte(withTargetID))
		}))
		defer srv.Close()

		out := run(t, "target-id", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		assert.Contains(t, out, "Target migration ID: 12345")
	})

	t.Run("--json emits a machine-readable object", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(withTargetID))
		}))
		defer srv.Close()

		out := run(t, "target-id", "--migration-id", "mig-1", "--json",
			"--source-url", srv.URL, "--source-token", "tok")

		var got targetIDView
		require.NoError(t, json.Unmarshal([]byte(out), &got), "output is not valid JSON: %s", out)
		assert.Equal(t, "mig-1", got.MigrationID)
		assert.Equal(t, int64(12345), got.TargetMigrationID)
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

		out, err := exec(t, "target-id", "--migration-id", "does-not-exist",
			"--source-url", srv.URL, "--source-token", "tok")
		require.Error(t, err, "expected an error for a missing migration, got output:\n%s", out)
		assert.Contains(t, err.Error(), "404")
		assert.NotContains(t, out, "Target migration ID", "missing migration must not print a target ID")
	})

	t.Run("requires a migration ID", func(t *testing.T) {
		err := runErr(t, "target-id", "--source-url", "https://x", "--source-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
	})

	t.Run("accepts the legacy lookup-target-id command", func(t *testing.T) {
		err := runErr(t, "lookup-target-id")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
		assert.NotContains(t, err.Error(), "unknown command")
	})
}

func TestWatchArguments(t *testing.T) {
	t.Run("accepts a positional migration ID", func(t *testing.T) {
		err := runErr(t, "watch", "mig-1", "--interval", "invalid")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid interval")
		assert.NotContains(t, err.Error(), "migration ID required")
	})

	t.Run("requires a migration ID", func(t *testing.T) {
		err := runErr(t, "watch")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration ID required")
	})
}

// elmapiCreateBody mirrors the create request body for assertions.
type elmapiCreateBody struct {
	SourceOrganizationLogin string `json:"source_organization_login"`
	SourceRepositoryName    string `json:"source_repository_name"`
	TargetOrganizationLogin string `json:"target_organization_login"`
	TargetRepositoryName    string `json:"target_repository_name"`
	TargetAPIEndpoint       string `json:"target_api_endpoint"`
	PATName                 string `json:"pat_name"`
	TargetVisibility        string `json:"target_visibility"`
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
