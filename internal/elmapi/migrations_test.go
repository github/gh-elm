package elmapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
