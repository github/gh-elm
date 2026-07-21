package migration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

		if gotMethod != http.MethodPost || !strings.HasSuffix(gotPath, "/enterprise/live-migrations") {
			t.Errorf("request = %s %s", gotMethod, gotPath)
		}
		// The target endpoint is derived from GH_TARGET_HOST (API-defect workaround).
		if gotBody.TargetAPIEndpoint != "https://api.example.ghe.com" {
			t.Errorf("target_api_endpoint = %q", gotBody.TargetAPIEndpoint)
		}
		// pat_name is stubbed with a sentinel (API-defect workaround).
		if gotBody.PATName != "BOGON" {
			t.Errorf("pat_name = %q", gotBody.PATName)
		}
		if gotBody.SourceRepositoryName != "web" || gotBody.TargetVisibility != "internal" {
			t.Errorf("body = %+v", gotBody)
		}
		if !strings.Contains(out, `"migration_id": "mig-1"`) {
			t.Errorf("output missing migration_id:\n%s", out)
		}
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

		if !startCalled {
			t.Error("start endpoint was not called")
		}
		if !strings.Contains(out, "created and started") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("rejects public visibility", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r", "--target-visibility", "public",
			"--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "private or internal") {
			t.Fatalf("expected visibility error, got %v", err)
		}
	})

	t.Run("--watch requires --start", func(t *testing.T) {
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--watch", "--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "--watch requires --start") {
			t.Fatalf("expected watch/start error, got %v", err)
		}
	})

	t.Run("requires the core flags", func(t *testing.T) {
		err := runErr(t, "create", "--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})

	t.Run("errors when no target host is configured (API-defect workaround)", func(t *testing.T) {
		t.Setenv("GH_TARGET_HOST", "")
		err := runErr(t, "create", "--source-org", "a", "--source-repo", "r",
			"--target-org", "b", "--target-repo", "r",
			"--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "requires a target endpoint") {
			t.Fatalf("expected target-endpoint error, got %v", err)
		}
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

		if !strings.HasSuffix(gotPath, "/enterprise/live-migrations/mig-1/start") {
			t.Errorf("path = %q", gotPath)
		}
		if !strings.Contains(out, "started") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runErr(t, "start", "--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "migration-id") {
			t.Fatalf("expected required-flag error, got %v", err)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("prints the raw API JSON", func(t *testing.T) {
		const respBody = `{"migration":{"migration_id":"mig-1","status":"in_progress"},"target_state":null,"combined_state":null,"messages":[]}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/enterprise/live-migrations/mig-1") {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := run(t, "status", "--migration-id", "mig-1",
			"--source-url", srv.URL, "--source-token", "tok")

		if strings.TrimSpace(out) != respBody {
			t.Errorf("output = %q, want raw JSON %q", out, respBody)
		}
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

		if !strings.Contains(gotQuery, "status=in_progress") || !strings.Contains(gotQuery, "page_size=25") {
			t.Errorf("query = %q", gotQuery)
		}
		if strings.TrimSpace(out) != respBody {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("rejects an invalid status", func(t *testing.T) {
		err := runErr(t, "list", "--status", "bogus",
			"--source-url", "https://x", "--source-token", "tok")
		if err == nil || !strings.Contains(err.Error(), "invalid --status") {
			t.Fatalf("expected status error, got %v", err)
		}
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
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tc.respCode)
				if tc.respBody != "" {
					_, _ = w.Write([]byte(tc.respBody))
				}
			}))
			defer srv.Close()

			args := append(tc.args, "--source-url", srv.URL, "--source-token", "tok")
			out := run(t, args...)

			if !strings.HasSuffix(gotPath, tc.wantPath) {
				t.Errorf("path = %q, want %q", gotPath, tc.wantPath)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Errorf("output = %q, want to contain %q", out, tc.wantOut)
			}
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

		if !gotForce {
			t.Error("expected force=true in cutover body")
		}
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
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
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
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected annotated auth error, got %v", err)
	}
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
	if err != nil {
		t.Fatalf("migration %v: %v\noutput:\n%s", args, err, out)
	}
	return out
}

// runErr executes a `migration` subcommand and returns the error (if any).
func runErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := exec(t, args...)
	return err
}

func exec(t *testing.T, args ...string) (string, error) {
	t.Helper()
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
