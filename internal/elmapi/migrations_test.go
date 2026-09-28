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

func TestMigrationDetailTerminalFailure(t *testing.T) {
	const body = `{
  "migration": {"migration_id": "mig-1", "status": "failed"},
  "target_state": {
    "status": "failed",
    "terminal_failure": {
      "code": "repository_policy",
      "summary": "Policy blocked it.",
      "occurred_at": "2026-09-04T12:58:37Z"
    }
  },
  "combined_state": {
    "status": "failed",
    "terminal_failure": {"code": "critical_resource", "summary": "Resource failed.", "occurred_at": null}
  }
}`

	newServer := func(t *testing.T, payload string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(payload))
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	t.Run("decodes the cause on both states", func(t *testing.T) {
		srv := newServer(t, body)

		detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
		require.NoError(t, err)

		require.NotNil(t, detail.TargetState.TerminalFailure)
		assert.Equal(t, "repository_policy", detail.TargetState.TerminalFailure.Code)
		assert.Equal(t, "Policy blocked it.", detail.TargetState.TerminalFailure.Summary)
		require.NotNil(t, detail.TargetState.TerminalFailure.OccurredAt)
		assert.Equal(t, "2026-09-04T12:58:37Z", *detail.TargetState.TerminalFailure.OccurredAt)

		require.NotNil(t, detail.CombinedState.TerminalFailure)
		assert.Equal(t, "critical_resource", detail.CombinedState.TerminalFailure.Code)
	})

	t.Run("leaves a null occurred_at nil so it stays distinct from the epoch", func(t *testing.T) {
		srv := newServer(t, body)

		detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
		require.NoError(t, err)

		require.NotNil(t, detail.CombinedState.TerminalFailure)
		assert.Nil(t, detail.CombinedState.TerminalFailure.OccurredAt)
	})

	t.Run("leaves the cause nil when the server omits it", func(t *testing.T) {
		srv := newServer(t, `{"migration":{"migration_id":"mig-1"},"target_state":{},"combined_state":{}}`)

		detail, err := NewClient(srv.URL, "tok").GetMigrationDetail(t.Context(), "mig-1")
		require.NoError(t, err)

		assert.Nil(t, detail.TargetState.TerminalFailure)
		assert.Nil(t, detail.CombinedState.TerminalFailure)
	})

	t.Run("preserves the cause verbatim in the raw document", func(t *testing.T) {
		srv := newServer(t, body)

		raw, err := NewClient(srv.URL, "tok").GetMigration(t.Context(), "mig-1")
		require.NoError(t, err)

		// `--json` writes this document straight through, so the contract
		// reaches scripts without the typed structs having to model it.
		assert.JSONEq(t, body, string(raw))
	})
}
