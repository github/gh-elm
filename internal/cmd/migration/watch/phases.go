// Package watch implements the live-updating migration watch display.
//
// It is a close port of the elm-exporter TWIRP CLI watch package
// (cmd/elm/cmd/watch), adapted to gh-elm's REST client: the proto
// GetMigrationStatus/GetCutoverStatus responses are replaced by the single
// elmapi.MigrationDetail document (GET /enterprise/live-migrations/:id), and
// proto enums are replaced by the REST wire strings emitted by github/github's
// live_migrations_serializer.rb.
package watch

import (
	"github.com/github/gh-elm/internal/elmapi"
)

// Phase represents a lifecycle stage in the migration timeline.
// These are the ordered phases displayed in the vertical timeline.
type Phase int

// Phase constants representing the stages of a migration lifecycle.
// Only lifecycle phases appear here; overlay states are separate.
const (
	// PhaseCreated indicates the migration has been created but not yet started.
	PhaseCreated Phase = iota
	// PhaseValidating indicates preflight validation is in progress.
	PhaseValidating
	// PhaseQueued indicates the migration is queued, waiting to begin.
	PhaseQueued
	// PhaseExporting indicates data is being exported from the source.
	PhaseExporting
	// PhaseBackfilling indicates data is being backfilled to the target.
	PhaseBackfilling
	// PhaseReadyForCutover indicates backfill is complete and cutover can begin.
	PhaseReadyForCutover
	// PhaseCuttingOver indicates the cutover process is in progress.
	PhaseCuttingOver
	// PhaseCompleted indicates the migration finished successfully.
	PhaseCompleted
)

// String returns a human-readable name for the phase.
func (p Phase) String() string {
	switch p {
	case PhaseCreated:
		return "Created"
	case PhaseValidating:
		return "Validating"
	case PhaseQueued:
		return "Queued"
	case PhaseExporting:
		return "Exporting"
	case PhaseBackfilling:
		return "Backfill"
	case PhaseReadyForCutover:
		return "Ready for Cutover"
	case PhaseCuttingOver:
		return "Cutover"
	case PhaseCompleted:
		return "Complete"
	default:
		return "Unknown"
	}
}

// Overlay represents a non-lifecycle state that is displayed on top of
// the current lifecycle phase (e.g. Failed, Paused, Degraded).
type Overlay int

// Overlay constants for states that modify the timeline display
// without changing the lifecycle position.
const (
	// OverlayNone indicates no overlay is active.
	OverlayNone Overlay = iota
	// OverlayFailed indicates the migration has failed.
	OverlayFailed
	// OverlayTerminated indicates the migration was manually terminated.
	OverlayTerminated
	// OverlayPaused indicates the migration is paused.
	OverlayPaused
	// OverlayDegraded indicates the migration is running in a degraded state.
	OverlayDegraded
)

// String returns a human-readable name for the overlay.
func (o Overlay) String() string {
	switch o {
	case OverlayNone:
		return ""
	case OverlayFailed:
		return "Failed"
	case OverlayTerminated:
		return "Cancelled"
	case OverlayPaused:
		return "Paused"
	case OverlayDegraded:
		return "Degraded"
	default:
		return "Unknown"
	}
}

// combined_state.status wire strings (github/github live_migrations_serializer.rb).
const (
	combinedCreated         = "created"
	combinedQueued          = "queued"
	combinedExporting       = "exporting"
	combinedProcessing      = "processing"
	combinedReadyForCutover = "ready_for_cutover"
	combinedCuttingOver     = "cutting_over"
	combinedCompleted       = "completed"
	combinedFailed          = "failed"
	combinedPaused          = "paused"
	combinedTerminated      = "terminated"
	combinedDegraded        = "degraded"
	combinedValidating      = "validating"
)

// migration.status wire strings (github/github live_migrations_serializer.rb).
const (
	statusCreated           = "created"
	statusQueued            = "queued"
	statusInProgress        = "in_progress"
	statusPaused            = "paused"
	statusCompleted         = "completed"
	statusFailed            = "failed"
	statusTerminated        = "terminated"
	statusValidating        = "validating"
	statusCutoverPending    = "cutover_pending"
	statusCutoverFinalizing = "cutover_finalizing"
)

// DerivePhase determines the current lifecycle phase and any overlay state
// from the migration status document. The returned Phase is always one of the
// ordered lifecycle phases (Created through Completed). The Overlay indicates
// terminal or exceptional states like Failed, Paused, etc.
func DerivePhase(detail *elmapi.MigrationDetail) (Phase, Overlay) {
	if detail == nil {
		return PhaseCreated, OverlayNone
	}

	// Primary: use combined_state.status if available.
	if cs := detail.CombinedState; cs != nil && cs.Status != nil && *cs.Status != "" {
		return phaseFromCombinedStatus(*cs.Status, detail)
	}

	// Fallback: derive from migration.status + target_state.
	return phaseFromFallback(detail)
}

func phaseFromCombinedStatus(cs string, detail *elmapi.MigrationDetail) (Phase, Overlay) {
	switch cs {
	case combinedValidating:
		return PhaseValidating, OverlayNone
	case combinedQueued:
		return PhaseQueued, OverlayNone
	case combinedExporting:
		return PhaseExporting, OverlayNone
	case combinedProcessing:
		return PhaseBackfilling, OverlayNone
	case combinedReadyForCutover:
		return PhaseReadyForCutover, OverlayNone
	case combinedCuttingOver:
		return PhaseCuttingOver, OverlayNone
	case combinedCompleted:
		return PhaseCompleted, OverlayNone
	case combinedFailed:
		return inferBasePhase(detail), OverlayFailed
	case combinedTerminated:
		return inferBasePhase(detail), OverlayTerminated
	case combinedPaused:
		return inferBasePhase(detail), OverlayPaused
	case combinedDegraded:
		return inferBasePhase(detail), OverlayDegraded
	default: // includes "created" and any unknown status
		return PhaseCreated, OverlayNone
	}
}

func phaseFromFallback(detail *elmapi.MigrationDetail) (Phase, Overlay) {
	info := detail.Migration
	if info == nil {
		return PhaseCreated, OverlayNone
	}

	migStatus := ""
	if info.Status != nil {
		migStatus = *info.Status
	}

	// Terminal/overlay states: derive base phase from target state progress,
	// then return the appropriate overlay.
	switch migStatus {
	case statusCompleted:
		return PhaseCompleted, OverlayNone
	case statusFailed:
		return inferBasePhase(detail), OverlayFailed
	case statusTerminated:
		return inferBasePhase(detail), OverlayTerminated
	case statusPaused:
		return inferBasePhase(detail), OverlayPaused
	case statusCreated:
		return PhaseCreated, OverlayNone
	case statusValidating:
		return PhaseValidating, OverlayNone
	case statusQueued:
		return PhaseQueued, OverlayNone
	case statusCutoverPending, statusCutoverFinalizing:
		return PhaseCuttingOver, OverlayNone
	}

	// Check target state for backfill progress.
	if rp := firstProgress(detail); rp != nil {
		if rp.AllResourcesSent && rp.BackfillResourcesProcessed >= rp.BackfillResourcesAdded {
			return PhaseReadyForCutover, OverlayNone
		}
		return PhaseBackfilling, OverlayNone
	}

	// in_progress with no target data = exporting.
	if migStatus == statusInProgress {
		return PhaseExporting, OverlayNone
	}

	return PhaseCreated, OverlayNone
}

// inferBasePhase determines the lifecycle phase from target state progress,
// ignoring the terminal migration status. Used when the migration is in a
// terminal/overlay state but we need to know where in the lifecycle it was.
func inferBasePhase(detail *elmapi.MigrationDetail) Phase {
	if rp := firstProgress(detail); rp != nil {
		if rp.AllResourcesSent && rp.BackfillResourcesProcessed >= rp.BackfillResourcesAdded {
			return PhaseReadyForCutover
		}
		return PhaseBackfilling
	}

	// No target data: best guess is exporting.
	return PhaseExporting
}

// firstProgress returns the first repository's progress counters, or nil when
// no target progress is available.
func firstProgress(detail *elmapi.MigrationDetail) *elmapi.RepositoryProgress {
	ts := detail.TargetState
	if ts == nil || len(ts.RepositoryProgress) == 0 {
		return nil
	}
	return &ts.RepositoryProgress[0]
}
