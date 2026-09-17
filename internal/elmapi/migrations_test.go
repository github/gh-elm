package elmapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMigrationDetail(t *testing.T) {
	t.Run("decodes nullable source archive observation independently of progress", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want *bool
		}{
			{"true", `{"source_repository_archived":true}`, new(true)},
			{"false", `{"source_repository_archived":false}`, new(false)},
			{"null", `{"source_repository_archived":null}`, nil},
			{"absent", `{}`, nil},
			{"null document", `null`, nil},
			{"completed with disagreeing legacy progress", `{"source_repository_archived":false,"migration":{"status":"completed"},"target_state":{"repository_progress":[{"repository_locked":true}]}}`, new(false)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Equal(t, "/enterprise/live-migrations/mig-1", r.URL.Path)
					_, _ = w.Write([]byte(tc.body))
				}))
				t.Cleanup(srv.Close)

				detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
				require.NoError(t, err)
				assert.Equal(t, tc.want, detail.SourceRepositoryArchived)
			})
		}
	})

	t.Run("rejects invalid observation types", func(t *testing.T) {
		for _, value := range []string{`"true"`, `1`, `{}`, `[]`} {
			t.Run(value, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"source_repository_archived":` + value + `}`))
				}))
				t.Cleanup(srv.Close)

				detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
				var typeError *json.UnmarshalTypeError
				require.ErrorAs(t, err, &typeError)
				assert.Equal(t, "source_repository_archived", typeError.Field)
				assert.Nil(t, detail)
			})
		}
	})

	t.Run("preserves whole request failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
		require.ErrorContains(t, err, "503")
		assert.Nil(t, detail)
	})
}

func TestMigrationResponses(t *testing.T) {
	t.Run("create retains raw JSON while decoding typed fields", func(t *testing.T) {
		const body = `{"migration_id":"mig-1","expires_at":null,"future_field":"preserved"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()

		resp, err := NewClient(srv.URL, "tok").CreateMigration(t.Context(), CreateMigrationRequest{})
		require.NoError(t, err)

		assert.Equal(t, "mig-1", resp.Value.MigrationID)
		assert.Equal(t, body, string(resp.Raw)) //nolint:testifylint // exact raw response retention
	})

	t.Run("list retains raw JSON while decoding migrations and pagination", func(t *testing.T) {
		const body = `{"migrations":[{"migration_id":"mig-1"}],"total_count":2,"next_cursor":"next","future_field":true}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()

		resp, err := NewClient(srv.URL, "tok").ListMigrations(t.Context(), ListMigrationsOptions{})
		require.NoError(t, err)

		require.Len(t, resp.Value.Migrations, 1)
		assert.Equal(t, "mig-1", resp.Value.Migrations[0].MigrationID)
		assert.Equal(t, int64(2), resp.Value.TotalCount)
		assert.Equal(t, "next", resp.Value.NextCursor)
		assert.Equal(t, body, string(resp.Raw)) //nolint:testifylint // exact raw response retention
	})

	t.Run("revert retains raw JSON while decoding typed fields", func(t *testing.T) {
		const body = `{"success":true,"unarchived_source_repository":true,"future_field":"preserved"}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()

		resp, err := NewClient(srv.URL, "tok").RevertCutover(t.Context(), "mig-1")
		require.NoError(t, err)

		assert.True(t, resp.Value.Success)
		assert.True(t, resp.Value.UnarchivedSourceRepository)
		assert.Equal(t, body, string(resp.Raw))
	})
}
