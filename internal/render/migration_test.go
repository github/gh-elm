package render

import (
	"bytes"
	"errors"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/theme"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestMigrationCreate(t *testing.T) {
	t.Run("renders a successful creation", func(t *testing.T) {
		expiresAt := "2026-09-01T16:34:20Z"

		assert.Equal(t, `Migration successfully created
  Migration ID        897930cf-51cb-4e2d-9806-6357a6e66b55
  Expires             2026-09-01T16:34:20Z

`, MigrationCreate(elmapi.CreateMigrationResponse{
			MigrationID: "897930cf-51cb-4e2d-9806-6357a6e66b55",
			ExpiresAt:   &expiresAt,
		}))
	})

	t.Run("uses semantic styles when color is enabled", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		expiresAt := "2026-09-01T16:34:20Z"
		styles := theme.New()
		output := MigrationCreate(elmapi.CreateMigrationResponse{
			MigrationID: "mig-1",
			ExpiresAt:   &expiresAt,
		})

		assert.Contains(t, output, styles.Success.Bold(true).Render("Migration successfully created"))
		assert.Contains(t, output, styles.Bold.Render("mig-1"))
		assert.Contains(t, output, styles.Muted.Render("  Expires             "+expiresAt))
	})
}

func TestMigrationCancel(t *testing.T) {
	t.Run("renders a successful cancellation", func(t *testing.T) {
		assert.Equal(t, "✓ Migration mig-1 cancelled.\n\n", MigrationCancel("mig-1"))
	})

	t.Run("renders the checkmark as a success when color is enabled", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		output := MigrationCancel("mig-1")

		assert.Contains(t, output, theme.New().Success.Render("✓"))
		assert.NotContains(t, output, theme.New().Success.Render("Migration mig-1 cancelled."))
	})
}

func TestMigrationStatus(t *testing.T) {
	t.Run("renders nested status sections", func(t *testing.T) {
		status := "in_progress"
		phase := "backfill"
		created := "2026-08-06T08:00:00Z"
		output := MigrationStatus(elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{
				MigrationID: "mig-1",
				Status:      &status,
				CreatedAt:   &created,
			},
			TargetState: &elmapi.TargetState{
				RepositoryProgress: []elmapi.RepositoryProgress{{
					RepositoryNWO:              "octo/repo",
					BackfillResourcesAdded:     10,
					BackfillResourcesProcessed: 8,
				}},
			},
			CombinedState: &elmapi.CombinedState{
				ReadyForCutover: false,
				CutoverBlockers: []string{"backfill incomplete"},
				Repositories: []elmapi.CombinedRepositoryState{{
					RepositoryNWO: "octo/repo",
					Phase:         &phase,
					DisplayStatus: "In progress",
				}},
			},
			Messages: []elmapi.MigrationMessage{{
				MessageType: "info",
				Message:     "Migration is running",
			}},
		})

		for _, want := range []string{
			"Migration", "mig-1", "Progress · octo/repo",
			"✓ Target available", "○ Not ready for cutover", "backfill incomplete",
			"Repository states", "Messages", "Migration is running",
		} {
			assert.Contains(t, output, want)
		}
	})

	t.Run("renders empty response explicitly", func(t *testing.T) {
		assert.Equal(t, "No migration status data returned.\n", MigrationStatus(elmapi.MigrationDetail{}))
	})
}

func TestMigrationList(t *testing.T) {
	t.Run("renders rows and pagination", func(t *testing.T) {
		status := "completed"
		output := MigrationList(elmapi.ListMigrationsResponse{
			Migrations: []elmapi.MigrationSummary{{
				MigrationID:             "mig-1",
				Status:                  &status,
				SourceOrganizationLogin: "source",
				SourceRepositoryName:    "repo",
				TargetOrganizationLogin: "target",
				TargetRepositoryName:    "repo",
			}},
			TotalCount: 2,
			NextCursor: "next",
		})

		for _, want := range []string{"mig-1", "Completed", "source/repo", "target/repo", "Showing 1 of 2", "next"} {
			assert.Contains(t, output, want)
		}
		assert.NotContains(t, output, "╭")
	})

	t.Run("renders an empty state for no migrations", func(t *testing.T) {
		output := MigrationList(elmapi.ListMigrationsResponse{})

		assert.Equal(t, "Migrations\n  No migrations available.\n  Create one with `gh elm migration create --help`.\n\n", output)
	})

	t.Run("renders returned migrations even when total count is zero", func(t *testing.T) {
		output := MigrationList(elmapi.ListMigrationsResponse{
			Migrations: []elmapi.MigrationSummary{{MigrationID: "mig-1"}},
			TotalCount: 0,
		})

		assert.NotContains(t, output, "No migrations available.")
		assert.Contains(t, output, "mig-1")
	})

	t.Run("renders a page-empty state when a cursor page has no migrations but a positive total", func(t *testing.T) {
		output := MigrationList(elmapi.ListMigrationsResponse{
			TotalCount: 5,
		})

		assert.Contains(t, output, "No migrations returned for this page.")
		assert.NotContains(t, output, "No migrations available.")
		assert.NotContains(t, output, "Create one with")
	})

	t.Run("mutes the empty state hint", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		styles := theme.New()
		output := MigrationList(elmapi.ListMigrationsResponse{})

		assert.Contains(t, output, "  No migrations available.")
		assert.Contains(t, output, styles.Muted.Render("  Create one with `gh elm migration create --help`."))
		assert.NotContains(t, output, styles.Muted.Render("  No migrations available."))
	})
}

func TestStatusPresentation(t *testing.T) {
	t.Run("renders unhealthy terminal states as failures", func(t *testing.T) {
		for _, status := range []string{"failed", "terminated", "aborted", "expired"} {
			assert.Contains(t, statusGlyph(status), "✗")
		}
	})

	t.Run("renders degraded as an attention state", func(t *testing.T) {
		assert.Contains(t, statusGlyph("degraded"), "Ⅱ")
	})

	t.Run("uses semantic ANSI colors when color is enabled", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		styles := theme.New()
		fieldOutput := field("Migration ID", "mig-1")
		assert.Contains(t, fieldOutput, styles.Muted.Render("Migration ID        "))
		assert.Contains(t, fieldOutput, styles.Muted.Render("Migration ID        ")+"mig-1")
		assert.NotContains(t, fieldOutput, styles.Muted.Render("mig-1"))
		assert.Contains(t, positiveState(true, "complete", "pending").glyph, styles.Success.Render("✓"))
		assert.Contains(t, failureState(false, "available", "unavailable").glyph, styles.Failure.Render("✗"))
		assert.Equal(t, styles.Success.Bold(true).Render("Completed"), statusText("completed"))
		assert.Equal(t, styles.Failure.Bold(true).Render("Failed"), statusText("failed"))
	})
}

func TestOutputWriters(t *testing.T) {
	t.Run("human output surfaces writer errors", func(t *testing.T) {
		assert.Error(t, Write(failWriter{}, "output"))
	})

	t.Run("raw JSON surfaces writer errors", func(t *testing.T) {
		assert.Error(t, WriteRawJSON(failWriter{}, []byte(`{"ok":true}`)))
	})

	t.Run("raw JSON appends a newline", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, WriteRawJSON(&output, []byte(`{"ok":true}`)))
		assert.Equal(t, "{\"ok\":true}\n", output.String())
	})
}
