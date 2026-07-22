package watch

import (
	"strings"
	"testing"
	"time"

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
			if phase != tc.phase || overlay != tc.overlay {
				t.Errorf("DerivePhase(%q) = (%v, %v), want (%v, %v)", tc.status, phase, overlay, tc.phase, tc.overlay)
			}
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
	if phase != PhaseBackfilling || overlay != OverlayFailed {
		t.Fatalf("got (%v, %v), want (Backfill, Failed)", phase, overlay)
	}
}

func TestDerivePhase_Fallback(t *testing.T) {
	// No combined state: fall back to migration.status.
	detail := &elmapi.MigrationDetail{
		Migration: &elmapi.MigrationSummary{Status: new(statusCutoverPending)},
	}
	phase, overlay := DerivePhase(detail)
	if phase != PhaseCuttingOver || overlay != OverlayNone {
		t.Fatalf("cutover_pending fallback = (%v, %v)", phase, overlay)
	}

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
	if phase != PhaseReadyForCutover {
		t.Fatalf("all-sent fallback = %v, want ReadyForCutover", phase)
	}
}

func TestDerivePhase_Nil(t *testing.T) {
	if phase, overlay := DerivePhase(nil); phase != PhaseCreated || overlay != OverlayNone {
		t.Fatalf("nil detail = (%v, %v)", phase, overlay)
	}
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
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q in:\n%s", want, out)
		}
	}
}

func TestView_Loading(t *testing.T) {
	m := New("id", time.Second, nil)
	if got := m.View(); !strings.Contains(got, "Loading migration status") {
		t.Fatalf("expected loading view, got %q", got)
	}
}
