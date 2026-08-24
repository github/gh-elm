package target

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

func TestMigrationList(t *testing.T) {
	t.Run("lists migrations in human-readable form", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z","repositories":["octo/repo"]}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "list", "--target-url", srv.URL, "--target-token", "tok")

		assert.Contains(t, out, "Migration ID: 1")
		assert.Contains(t, out, "Status:       in progress")
		assert.Contains(t, out, "octo/repo")
		assert.Contains(t, out, "Found 1 migration.")
	})

	t.Run("emits newline-delimited JSON with --json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "list", "--json", "--target-url", srv.URL, "--target-token", "tok")

		assert.Contains(t, out, `"migrationId":"1"`)
		assert.NotContains(t, out, "Found ", "JSON output should not include the human summary")
	})

	t.Run("--json echoes the raw API JSON verbatim", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_PAUSED","expiresAt":"2024-01-01T00:00:00Z","correlationId":"abc-123"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := strings.TrimSpace(runMigration(t, "migration", "list", "--json", "--target-url", srv.URL, "--target-token", "tok"))

		assert.Contains(t, out, `"correlationId":"abc-123"`, "expected unknown field preserved")
		for _, absent := range []string{"description", "repositories"} {
			assert.NotContains(t, out, absent, "re-marshaled zero field leaked into output")
		}
	})

	t.Run("filters client-side by --status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[` +
				`{"migrationId":"1","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"},` +
				`{"migrationId":"2","status":"STATUS_TYPE_PAUSED","expiresAt":"2024-01-01T00:00:00Z"}` +
				`],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "list", "--status", "paused", "--target-url", srv.URL, "--target-token", "tok")

		assert.NotContains(t, out, "Migration ID: 1")
		assert.Contains(t, out, "Migration ID: 2")
		assert.Contains(t, out, "Found 1 paused migration.")
	})

	t.Run("filter-aware summary on an empty filtered result", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "list", "--status", "paused", "--target-url", srv.URL, "--target-token", "tok")

		assert.Contains(t, out, "No paused migrations found.")
	})

	t.Run("respects --max-results", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1"},{"migrationId":"2"},{"migrationId":"3"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "list", "--max-results", "2", "--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, 2, strings.Count(out, "Migration ID:"), "expected 2 migrations")
	})

	t.Run("rejects an invalid status", func(t *testing.T) {
		err := runMigrationErr(t, "migration", "list", "--status", "bogus",
			"--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --status")
	})

	t.Run("surfaces an actionable auth error on 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := runMigrationErr(t, "migration", "list", "--target-url", srv.URL, "--target-token", "bad")
		require.Error(t, err, "expected an error on 401")
		for _, want := range []string{"authentication failed", "401", "GH_TARGET_HOST", "GH_TARGET_TOKEN"} {
			assert.Contains(t, err.Error(), want)
		}
	})
}

func TestMigrationCreate(t *testing.T) {
	t.Run("creates a migration and prints human-readable confirmation", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migrationId":"42","expiresAt":"2024-01-01T00:00:00Z"}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "create",
			"--source-repository-url", "https://source.example/octo/repo",
			"--repository", "octo/repo",
			"--description", "test migration",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "https://source.example/octo/repo", gotBody["source_url"])
		assert.Equal(t, []any{"octo/repo"}, gotBody["repositories"])
		assert.Equal(t, "test migration", gotBody["description"])
		assert.Contains(t, out, "Migration 42 created.")
		assert.Contains(t, out, "Expires at:")
	})

	t.Run("emits formatted API JSON with --json", func(t *testing.T) {
		const respBody = `{"migrationId":"42","expiresAt":"2024-01-01T00:00:00Z"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(respBody))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "create",
			"--source-repository-url", "https://source.example/octo/repo",
			"--repository", "octo/repo", "--json",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "{\n  \"migrationId\": \"42\",\n  \"expiresAt\": \"2024-01-01T00:00:00Z\"\n}\n", out)
	})

	t.Run("requires --source-repository-url", func(t *testing.T) {
		err := runMigrationErr(t, "migration", "create", "--repository", "octo/repo",
			"--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source-repository-url")
	})

	t.Run("requires --repository", func(t *testing.T) {
		err := runMigrationErr(t, "migration", "create", "--source-repository-url", "https://source.example/octo/repo",
			"--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "repository")
	})

	t.Run("surfaces an actionable auth error on 401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := runMigrationErr(t, "migration", "create",
			"--source-repository-url", "https://source.example/octo/repo", "--repository", "octo/repo",
			"--target-url", srv.URL, "--target-token", "bad")
		require.Error(t, err, "expected an error on 401")
		assert.Contains(t, err.Error(), "authentication failed")
	})
}

func TestMigrationStatus(t *testing.T) {
	t.Run("prints human-readable status by default", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z","repositoryProgress":[{"repositoryNwo":"octo/repo","resourcesAdded":10,"resourcesProcessed":5,"eventsAdded":2,"eventsProcessed":1}]}}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "status", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "/enterprise/migration/42/status", gotPath)
		assert.Contains(t, out, "Migration ID: 42")
		assert.Contains(t, out, "Status:       in progress")
		assert.Contains(t, out, "Progress (octo/repo): resources 5/10 processed, events 1/2 processed")
	})

	t.Run("emits the raw API JSON with --json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"}}`))
		}))
		defer srv.Close()

		out := strings.TrimSpace(runMigration(t, "migration", "status", "--migration-id", "42", "--json",
			"--target-url", srv.URL, "--target-token", "tok"))

		assert.Contains(t, out, `"migrationId":"42"`)
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		err := runMigrationErr(t, "migration", "status", "--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration-id")
	})

	t.Run("accepts the -m shorthand", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"}}`))
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "status", "-m", "42", "--target-url", srv.URL, "--target-token", "tok")
		assert.Contains(t, out, "Migration ID: 42")
	})

	t.Run("rejects a non-positive --migration-id locally", func(t *testing.T) {
		err := runMigrationErr(t, "migration", "status", "--migration-id", "0",
			"--target-url", "https://x", "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "positive integer")
	})

	t.Run("surfaces an actionable error on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		err := runMigrationErr(t, "migration", "status", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		for _, want := range []string{"404", "not found", "lookup-target-id"} {
			assert.Contains(t, err.Error(), want)
		}
	})
}

func TestMigrationPauseResumeAbort(t *testing.T) {
	t.Run("pause posts to the pause endpoint and prints confirmation", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "pause", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "/enterprise/migration/42/pause", gotPath)
		assert.Contains(t, out, "Migration 42 paused.")
	})

	t.Run("resume posts to the resume endpoint and prints confirmation", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "resume", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "/enterprise/migration/42/resume", gotPath)
		assert.Contains(t, out, "Migration 42 resumed.")
	})

	t.Run("abort posts to the abort endpoint and prints confirmation", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		out := runMigration(t, "migration", "abort", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")

		assert.Equal(t, "/enterprise/migration/42/abort", gotPath)
		assert.Contains(t, out, "Migration 42 aborted.")
	})

	t.Run("requires --migration-id", func(t *testing.T) {
		for _, sub := range []string{"pause", "resume", "abort"} {
			err := runMigrationErr(t, "migration", sub, "--target-url", "https://x", "--target-token", "tok")
			require.Error(t, err, "expected error for %s", sub)
			assert.Contains(t, err.Error(), "migration-id")
		}
	})

	t.Run("rejects a non-positive --migration-id locally for each subcommand", func(t *testing.T) {
		for _, sub := range []string{"pause", "resume", "abort"} {
			err := runMigrationErr(t, "migration", sub, "--migration-id", "-1",
				"--target-url", "https://x", "--target-token", "tok")
			require.Errorf(t, err, "expected error for %s", sub)
			assert.Contains(t, err.Error(), "positive integer")
		}
	})

	t.Run("accepts the -m shorthand", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		runMigration(t, "migration", "pause", "-m", "42", "--target-url", srv.URL, "--target-token", "tok")
		assert.Equal(t, "/enterprise/migration/42/pause", gotPath)
	})

	t.Run("surfaces an actionable error on 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		err := runMigrationErr(t, "migration", "pause", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		for _, want := range []string{"404", "not found", "lookup-target-id"} {
			assert.Contains(t, err.Error(), want)
		}
	})

	t.Run("surfaces a precondition-failed error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not pausable", http.StatusPreconditionFailed)
		}))
		defer srv.Close()

		err := runMigrationErr(t, "migration", "pause", "--migration-id", "42",
			"--target-url", srv.URL, "--target-token", "tok")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "412")
	})
}

// runMigration executes a `target` subcommand and returns output, failing on
// error.
func runMigration(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execMigration(t, args...)
	require.NoErrorf(t, err, "target %v output:\n%s", args, out)
	return out
}

// runMigrationErr executes a `target` subcommand and returns the error (if any).
func runMigrationErr(t *testing.T, args ...string) error {
	_, err := execMigration(t, args...)
	return err
}

func execMigration(t *testing.T, args ...string) (string, error) {
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
