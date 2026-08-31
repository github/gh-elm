package elmapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTargetMigration(t *testing.T) {
	t.Run("sends the request body and decodes the raw response", func(t *testing.T) {
		var gotPath, gotMethod string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"migrationId":"42","expiresAt":"2024-01-01T00:00:00Z"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		raw, err := c.CreateTargetMigration(t.Context(), CreateTargetMigrationRequest{
			SourceURL:             "https://source.example/octo/repo",
			Repositories:          []string{"octo/repo"},
			Description:           "test migration",
			ExporterMigrationGUID: "11111111-1111-1111-1111-111111111111",
		})
		require.NoError(t, err, "CreateTargetMigration")

		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Equal(t, "/enterprise/migration/create", gotPath)
		assert.Equal(t, "https://source.example/octo/repo", gotBody["source_url"])
		assert.Equal(t, []any{"octo/repo"}, gotBody["repositories"])
		assert.Equal(t, "test migration", gotBody["description"])
		assert.JSONEq(t, `{"migrationId":"42","expiresAt":"2024-01-01T00:00:00Z"}`, string(raw))
	})

	t.Run("returns HTTPError on non-201", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadRequest)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.CreateTargetMigration(t.Context(), CreateTargetMigrationRequest{
			SourceURL:    "https://source.example/octo/repo",
			Repositories: []string{"octo/repo"},
		})
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
	})
}

func TestListTargetMigrations(t *testing.T) {
	t.Run("sends pagination params and decodes a page", func(t *testing.T) {
		var gotPath, gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		resp, err := c.ListTargetMigrations(t.Context(), ListTargetMigrationsOptions{
			PageSize:  50,
			PageToken: "cursor",
		})
		require.NoError(t, err, "ListTargetMigrations")

		assert.Equal(t, "/enterprise/migration/list", gotPath)
		parsed, err := url.ParseQuery(gotQuery)
		require.NoError(t, err, "parse query")
		assert.Equal(t, "50", parsed.Get("page_size"))
		assert.Equal(t, "cursor", parsed.Get("page_token"))
		require.Len(t, resp.Migrations, 1)
		assert.Equal(t, "1", resp.Migrations[0].MigrationID)
		assert.Equal(t, TargetMigrationStatusInProgress, resp.Migrations[0].Status)
	})

	t.Run("omits empty pagination params", func(t *testing.T) {
		var gotQuery string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"migrations":[],"nextPageToken":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.ListTargetMigrations(t.Context(), ListTargetMigrationsOptions{})
		require.NoError(t, err, "ListTargetMigrations")
		assert.Empty(t, gotQuery, "expected empty query")
	})

	t.Run("returns HTTPError on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.ListTargetMigrations(t.Context(), ListTargetMigrationsOptions{})
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusInternalServerError, httpErr.StatusCode)
	})

	t.Run("--json echoes the raw API JSON verbatim", func(t *testing.T) {
		// The API response carries a field TargetMigration does not model
		// (correlationId) and omits description/repositories. Raw must
		// preserve the unknown field and must not gain fabricated zero values.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1","status":"STATUS_TYPE_PAUSED","expiresAt":"2024-01-01T00:00:00Z","correlationId":"abc-123"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		resp, err := c.ListTargetMigrations(t.Context(), ListTargetMigrationsOptions{})
		require.NoError(t, err, "ListTargetMigrations")
		require.Len(t, resp.Migrations, 1)
		raw := string(resp.Migrations[0].Raw)
		assert.Contains(t, raw, `"correlationId":"abc-123"`, "expected unknown field preserved")
		for _, absent := range []string{"description", "repositories"} {
			assert.NotContains(t, raw, absent, "re-marshaled zero field leaked into raw")
		}
	})
}

func TestIterTargetMigrations(t *testing.T) {
	t.Run("follows pagination across pages", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("page_token") {
			case "":
				_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1"},{"migrationId":"2"}],"nextPageToken":"page2"}`))
			case "page2":
				_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"3"}],"nextPageToken":""}`))
			default:
				assert.Failf(t, "unexpected page_token", "%q", r.URL.Query().Get("page_token"))
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		var ids []string
		for m, err := range c.IterTargetMigrations(t.Context(), ListTargetMigrationsOptions{}) {
			require.NoError(t, err, "iter")
			ids = append(ids, m.MigrationID)
		}
		assert.Equal(t, []string{"1", "2", "3"}, ids)
	})

	t.Run("stops early when caller breaks", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1"},{"migrationId":"2"}],"nextPageToken":"more"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		for _, err := range c.IterTargetMigrations(t.Context(), ListTargetMigrationsOptions{}) {
			require.NoError(t, err, "iter")
			count++
			break
		}
		assert.Equal(t, 1, count)
	})

	t.Run("surfaces errors", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		var gotErr error
		for _, err := range c.IterTargetMigrations(t.Context(), ListTargetMigrationsOptions{}) {
			gotErr = err
		}
		assert.Error(t, gotErr, "expected an error from iteration")
	})

	t.Run("errors instead of looping forever when the API repeats a cursor", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls > 5 {
				http.Error(w, "IterTargetMigrations did not stop on a repeated cursor", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"migrations":[],"nextPageToken":"never-ending"}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		count := 0
		var gotErr error
		for _, err := range c.IterTargetMigrations(t.Context(), ListTargetMigrationsOptions{}) {
			count++
			gotErr = err
		}
		assert.Equal(t, 1, count, "expected the final (error) pair to be yielded")
		require.Error(t, gotErr, "a repeated cursor must surface as an error, not a quiet stop")
		assert.Contains(t, gotErr.Error(), "repeated page token")
		assert.Equal(t, 2, calls, "expected exactly 2 requests")
	})

	t.Run("stops immediately when the context is already canceled", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1"}],"nextPageToken":""}`))
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		c := NewClient(srv.URL, "tok")
		count := 0
		var gotErr error
		for _, err := range c.IterTargetMigrations(ctx, ListTargetMigrationsOptions{}) {
			count++
			gotErr = err
		}
		assert.Equal(t, 1, count, "expected the final (error) pair to be yielded")
		require.ErrorIs(t, gotErr, context.Canceled)
		assert.Zero(t, calls, "no request should be made once the context is already canceled")
	})

	t.Run("stops after the current page instead of following a next page token once canceled", func(t *testing.T) {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			_, _ = w.Write([]byte(`{"migrations":[{"migrationId":"1"}],"nextPageToken":"page2"}`))
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c := NewClient(srv.URL, "tok")

		var ids []string
		var gotErr error
		for m, err := range c.IterTargetMigrations(ctx, ListTargetMigrationsOptions{}) {
			if err != nil {
				gotErr = err
				break
			}
			ids = append(ids, m.MigrationID)
			cancel() // cancel once the first page's item has been yielded
		}
		assert.Equal(t, []string{"1"}, ids, "expected only the already-fetched page's item")
		require.ErrorIs(t, gotErr, context.Canceled)
		assert.Equal(t, 1, calls, "no second-page request should follow a cancellation")
	})
}

func TestGetTargetMigrationStatus(t *testing.T) {
	t.Run("decodes the migration and progress", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z","repositoryProgress":[{"repositoryNwo":"octo/repo","resourcesAdded":10,"resourcesProcessed":5}]}}`))
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		resp, err := c.GetTargetMigrationStatus(t.Context(), 42)
		require.NoError(t, err, "GetTargetMigrationStatus")

		assert.Equal(t, "/enterprise/migration/42/status", gotPath)
		assert.Equal(t, "42", resp.Migration.MigrationID)
		require.Len(t, resp.Migration.RepositoryProgress, 1)
		assert.Equal(t, "octo/repo", resp.Migration.RepositoryProgress[0].RepositoryNWO)
		assert.Equal(t, int64(10), resp.Migration.RepositoryProgress[0].ResourcesAdded)
	})

	t.Run("decodes string-encoded progress counts", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z","repositoryProgress":[{"repositoryNwo":"octo/repo","resourcesAdded":"10","resourcesProcessed":"5","eventsAdded":"4","eventsProcessed":"3","backfillResourcesAcknowledged":"2","liveUpdateResourcesAcknowledged":"1"}]}}`))
		}))
		defer srv.Close()

		resp, err := NewClient(srv.URL, "tok").GetTargetMigrationStatus(t.Context(), 42)

		require.NoError(t, err)
		require.Len(t, resp.Migration.RepositoryProgress, 1)
		progress := resp.Migration.RepositoryProgress[0]
		assert.Equal(t, int64(10), progress.ResourcesAdded)
		assert.Equal(t, int64(5), progress.ResourcesProcessed)
		assert.Equal(t, int64(4), progress.EventsAdded)
		assert.Equal(t, int64(3), progress.EventsProcessed)
		assert.Equal(t, int64(2), progress.BackfillResourcesAcknowledged)
		assert.Equal(t, int64(1), progress.LiveUpdateResourcesAcknowledged)
	})

	t.Run("rejects malformed progress counts", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"migration":{"migrationId":"42","status":"STATUS_TYPE_IN_PROGRESS","expiresAt":"2024-01-01T00:00:00Z","repositoryProgress":[{"resourcesAdded":"many"}]}}`))
		}))
		defer srv.Close()

		_, err := NewClient(srv.URL, "tok").GetTargetMigrationStatus(t.Context(), 42)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `invalid integer "many"`)
	})

	t.Run("returns HTTPError on non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		_, err := c.GetTargetMigrationStatus(t.Context(), 1)
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusNotFound, httpErr.StatusCode)
	})
}

func TestPauseResumeAbortTargetMigration(t *testing.T) {
	t.Run("pause posts to the pause path and expects 204", func(t *testing.T) {
		var gotPath, gotMethod string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		err := c.PauseTargetMigration(t.Context(), 42)
		require.NoError(t, err, "PauseTargetMigration")
		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Equal(t, "/enterprise/migration/42/pause", gotPath)
	})

	t.Run("resume posts to the resume path and expects 204", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		err := c.ResumeTargetMigration(t.Context(), 42)
		require.NoError(t, err, "ResumeTargetMigration")
		assert.Equal(t, "/enterprise/migration/42/resume", gotPath)
	})

	t.Run("abort posts to the abort path and expects 204", func(t *testing.T) {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		err := c.AbortTargetMigration(t.Context(), 42)
		require.NoError(t, err, "AbortTargetMigration")
		assert.Equal(t, "/enterprise/migration/42/abort", gotPath)
	})

	t.Run("returns HTTPError with 412 on precondition failed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not pausable", http.StatusPreconditionFailed)
		}))
		defer srv.Close()

		c := NewClient(srv.URL, "tok")
		err := c.PauseTargetMigration(t.Context(), 1)
		require.Error(t, err)
		var httpErr *HTTPError
		require.ErrorAs(t, err, &httpErr)
		assert.Equal(t, http.StatusPreconditionFailed, httpErr.StatusCode)
	})
}
