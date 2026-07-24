package watch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-elm/internal/elmapi"
)

func combined(status string) *elmapi.MigrationDetail {
	return &elmapi.MigrationDetail{CombinedState: &elmapi.CombinedState{Status: new(status)}}
}

func TestDerivePhase_FromCombinedStatus(t *testing.T) {
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
}

func TestDerivePhase_OverlayInfersBasePhase(t *testing.T) {
	// A failed migration mid-backfill should infer PhaseBackfilling under the
	// failed overlay from target progress.
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
}

func TestDerivePhase_Fallback(t *testing.T) {
	// No combined state: fall back to migration.status.
	detail := &elmapi.MigrationDetail{
		Migration: &elmapi.MigrationSummary{Status: new(statusCutoverPending)},
	}
	phase, overlay := DerivePhase(detail)
	assert.Equal(t, PhaseCuttingOver, phase, "cutover_pending fallback phase")
	assert.Equal(t, OverlayNone, overlay)

	// in_progress with backfill progress and all sent -> ready for cutover.
	detail = &elmapi.MigrationDetail{
		Migration: &elmapi.MigrationSummary{Status: new(statusInProgress)},
		TargetState: &elmapi.TargetState{
			RepositoryProgress: []elmapi.RepositoryProgress{{
				BackfillResourcesAdded:     10,
				BackfillResourcesProcessed: 10,
				AllResourcesSent:           true,
			}},
		},
	}
	phase, _ = DerivePhase(detail)
	assert.Equal(t, PhaseReadyForCutover, phase, "all-sent fallback")
}

func TestDerivePhase_Nil(t *testing.T) {
	phase, overlay := DerivePhase(nil)
	assert.Equal(t, PhaseCreated, phase)
	assert.Equal(t, OverlayNone, overlay)
}

func TestView_RendersTimelineAndProgress(t *testing.T) {
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
		"acme/web → acme-cloud/web", // header
		"Backfill",                  // active phase name
		"50 processed / 100 sent",   // progress bar line
		"Preflight",                 // preflight section
		"disk space: ok",            // parsed preflight message
		"hello world",               // generic message
		"Refreshing every 2s",       // footer
	} {
		assert.Contains(t, out, want)
	}
}

func TestView_Loading(t *testing.T) {
	m := New("id", time.Second, nil)
	assert.Contains(t, m.View(), "Loading migration status")
}
