package watch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/elmapi"
)

func combined(status string) *elmapi.MigrationDetail {
	return &elmapi.MigrationDetail{CombinedState: &elmapi.CombinedState{Status: new(status)}}
}

func TestDerivePhase(t *testing.T) {
	t.Run("from combined status", func(t *testing.T) {
		cases := []struct {
			status  string
			phase   Phase
			overlay Overlay
		}{
			{combinedCreated, PhaseCreated, OverlayNone},
			{combinedValidating, PhaseValidating, OverlayNone},
			{combinedQueued, PhaseQueued, OverlayNone},
			{combinedExporting, PhaseExporting, OverlayNone},
			{combinedProcessing, PhaseBackfilling, OverlayNone},
			{combinedReadyForCutover, PhaseReadyForCutover, OverlayNone},
			{combinedCuttingOver, PhaseCuttingOver, OverlayNone},
			{combinedCompleted, PhaseCompleted, OverlayNone},
			{combinedPaused, PhaseExporting, OverlayPaused},
			{combinedDegraded, PhaseExporting, OverlayDegraded},
		}
		for _, tc := range cases {
			t.Run(tc.status, func(t *testing.T) {
				phase, overlay := DerivePhase(combined(tc.status))
				assert.Equal(t, tc.phase, phase)
				assert.Equal(t, tc.overlay, overlay)
			})
		}
	})

	t.Run("overlay infers base phase from target progress", func(t *testing.T) {
		// A failed migration mid-backfill should infer PhaseBackfilling under
		// the failed overlay from target progress.
		detail := &elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{Status: new(combinedFailed)},
			TargetState: &elmapi.TargetState{
				RepositoryProgress: []elmapi.RepositoryProgress{{
					BackfillResourcesAdded:     100,
					BackfillResourcesProcessed: 40,
				}},
			},
		}
		phase, overlay := DerivePhase(detail)
		assert.Equal(t, PhaseBackfilling, phase)
		assert.Equal(t, OverlayFailed, overlay)
	})

	t.Run("cutover_pending falls back to CuttingOver", func(t *testing.T) {
		// No combined state: fall back to migration.status.
		detail := &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{Status: new(statusCutoverPending)},
		}
		phase, overlay := DerivePhase(detail)
		assert.Equal(t, PhaseCuttingOver, phase)
		assert.Equal(t, OverlayNone, overlay)
	})

	t.Run("in_progress with all-resources-sent falls back to ReadyForCutover", func(t *testing.T) {
		detail := &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{Status: new(statusInProgress)},
			TargetState: &elmapi.TargetState{
				RepositoryProgress: []elmapi.RepositoryProgress{{
					BackfillResourcesAdded:     10,
					BackfillResourcesProcessed: 10,
					AllResourcesSent:           true,
				}},
			},
		}
		phase, _ := DerivePhase(detail)
		assert.Equal(t, PhaseReadyForCutover, phase)
	})

	t.Run("nil detail defaults to Created/None", func(t *testing.T) {
		phase, overlay := DerivePhase(nil)
		assert.Equal(t, PhaseCreated, phase)
		assert.Equal(t, OverlayNone, overlay)
	})
}

func TestView(t *testing.T) {
	t.Run("source observation stays above timeline through cutover and completion", func(t *testing.T) {
		cases := []struct {
			name     string
			archived *bool
			want     string
		}{
			{"true", new(true), "Source repository archived"},
			{"false", new(false), "Source repository not archived"},
			{"unavailable", nil, "Source repository archive state unavailable"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				m := New("mig-1", time.Second, nil)
				m.width = 60
				m.detail = &elmapi.MigrationDetail{
					SourceRepositoryArchived: tc.archived,
					TargetState: &elmapi.TargetState{RepositoryProgress: []elmapi.RepositoryProgress{{
						RepositoryLocked:       true,
						InitialGitPushComplete: true,
						AllResourcesSent:       true,
					}}},
				}
				m.basePhase = PhaseCuttingOver
				out := m.View()
				assert.Contains(t, out, tc.want)
				assert.Equal(t, 1, strings.Count(out, "Source repository"))
				assert.Less(t, strings.Index(out, tc.want), strings.Index(out, "Created"))
				assert.NotContains(t, out, "Repo locked")
				assert.Contains(t, out, "Git push: ✓  All resources sent: ✓")

				m.basePhase = PhaseCompleted
				m.detail.TargetState = nil
				out = m.View()
				assert.Contains(t, out, tc.want)
				assert.Equal(t, 1, strings.Count(out, "Source repository"))
				assert.Contains(t, out, "Migration completed successfully.")
			})
		}
	})

	t.Run("source-only response and empty response preserve existing timeline", func(t *testing.T) {
		m := New("id", time.Second, nil)
		m.detail = &elmapi.MigrationDetail{SourceRepositoryArchived: new(false)}
		assert.Contains(t, m.View(), "Source repository not archived")
		m.detail = &elmapi.MigrationDetail{}
		assert.Contains(t, m.View(), "Source repository archive state unavailable")
		assert.Contains(t, m.View(), "Created")
	})

	t.Run("renders timeline and progress", func(t *testing.T) {
		m := New("11112222-3333-4444-5555-666677778888", 2*time.Second, nil)
		m.detail = &elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{
				SourceOrganizationLogin: "acme",
				SourceRepositoryName:    "web",
				TargetOrganizationLogin: "acme-cloud",
				TargetRepositoryName:    "web",
				TargetVisibility:        new("internal"),
				Status:                  new(statusInProgress),
				CreatedAt:               new("2024-01-01T00:00:00Z"),
				StartedAt:               new("2024-01-01T00:01:00Z"),
			},
			CombinedState: &elmapi.CombinedState{Status: new(combinedProcessing)},
			TargetState: &elmapi.TargetState{
				RepositoryProgress: []elmapi.RepositoryProgress{{
					RepositoryNWO:              "acme/web",
					BackfillResourcesAdded:     100,
					BackfillResourcesProcessed: 50,
				}},
			},
			Messages: []elmapi.MigrationMessage{
				{MessageType: "info", Message: "[preflight: disk space] ok"},
				{MessageType: "info", Message: "hello world"},
			},
		}

		m.basePhase, m.overlay = DerivePhase(m.detail)

		out := m.View()
		for _, want := range []string{
			"11112222-3333-4444-5555-666677778888", // full migration ID
			"acme/web → acme-cloud/web",            // header
			"Backfill",                             // active phase name
			"50 processed / 100 sent",              // progress bar line
			"Preflight",                            // preflight section
			"disk space: ok",                       // parsed preflight message
			"hello world",                          // generic message
			"Refreshing every 2s",                  // footer
		} {
			assert.Contains(t, out, want)
		}
		assert.NotContains(t, out, "11112222-3333...")
	})

	t.Run("loading", func(t *testing.T) {
		m := New("id", time.Second, nil)
		assert.Equal(t, "Loading migration status...\n", m.View())
	})
}

func TestUpdate(t *testing.T) {
	t.Run("successful refresh replaces archive observation and request failure retains it", func(t *testing.T) {
		type response struct {
			body   string
			status int
		}
		responses := make(chan response, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			next := <-responses
			w.WriteHeader(next.status)
			_, _ = w.Write([]byte(next.body))
		}))
		t.Cleanup(srv.Close)
		m := New("mig-1", time.Second, elmapi.NewClient(srv.URL, "tok"))
		cases := []struct {
			body string
			want *bool
			text string
		}{
			{`{"source_repository_archived":true}`, new(true), "Source repository archived"},
			{`{"source_repository_archived":false}`, new(false), "Source repository not archived"},
			{`{"source_repository_archived":true}`, new(true), "Source repository archived"},
			{`{"source_repository_archived":null}`, nil, "Source repository archive state unavailable"},
			{`{"source_repository_archived":true}`, new(true), "Source repository archived"},
			{`{}`, nil, "Source repository archive state unavailable"},
		}
		for _, tc := range cases {
			responses <- response{body: tc.body, status: http.StatusOK}
			msg := fetchStatus(m.client, m.migrationID, m.interval)
			updated, cmd := m.Update(msg)
			m = updated.(Model)
			require.NoError(t, m.fetchErr)
			require.NotNil(t, cmd)
			assert.Equal(t, tc.want, m.detail.SourceRepositoryArchived)
			assert.Contains(t, m.View(), tc.text)
		}

		responses <- response{body: `{"source_repository_archived":true}`, status: http.StatusOK}
		updated, _ := m.Update(fetchStatus(m.client, m.migrationID, m.interval))
		m = updated.(Model)
		lastDetail, lastUpdated := m.detail, m.lastUpdated
		responses <- response{status: http.StatusServiceUnavailable}
		updated, cmd := m.Update(fetchStatus(m.client, m.migrationID, m.interval))
		m = updated.(Model)
		require.Error(t, m.fetchErr)
		assert.NotNil(t, cmd)
		assert.Same(t, lastDetail, m.detail)
		assert.Equal(t, lastUpdated, m.lastUpdated)
		assert.Contains(t, m.View(), "Source repository archived")
		assert.Contains(t, m.View(), "Failed to refresh (retrying...)")
		assert.Contains(t, m.View(), "Last updated: "+formatTimestamp(lastUpdated))

		responses <- response{body: `{}`, status: http.StatusOK}
		updated, _ = m.Update(fetchStatus(m.client, m.migrationID, m.interval))
		m = updated.(Model)
		assert.NoError(t, m.fetchErr)
		assert.Nil(t, m.detail.SourceRepositoryArchived)
		assert.NotContains(t, m.View(), "Failed to refresh")
	})
}
