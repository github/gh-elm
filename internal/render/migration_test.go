package render

import (
	"bytes"
	"errors"
	"strings"
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

		assert.Equal(t, `✓ Migration successfully created
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

		assert.Contains(t, output, styles.Success.Render("✓")+" Migration successfully created")
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
			"Target", "✓ Available", "✗ Not ready for cutover", "backfill incomplete",
			"Repository states", "Messages", "Migration is running",
		} {
			assert.Contains(t, output, want)
		}
		assert.Less(t, strings.Index(output, "Visibility"), strings.Index(output, "Target"))
		assert.Less(t, strings.Index(output, "Target"), strings.Index(output, "Created"))
	})

	t.Run("renders empty response explicitly", func(t *testing.T) {
		assert.Equal(t, "No migration status data returned.\n", MigrationStatus(elmapi.MigrationDetail{}))
	})

	t.Run("deduplicates ready-for-cutover state", func(t *testing.T) {
		status := "ready_for_cutover"
		phase := "ready_for_cutover"

		output := MigrationStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{
				Status:          &status,
				DisplayMessage:  "Ready for cutover",
				ReadyForCutover: true,
				Repositories: []elmapi.CombinedRepositoryState{{
					RepositoryNWO: "elm-test/the-hook2",
					Phase:         &phase,
					DisplayStatus: "Ready for cutover",
				}},
			},
		})

		assert.Equal(t, `Cutover
  ✓ Ready for cutover

Repository states
  • elm-test/the-hook2 · Ready for cutover
`, output)
	})

	t.Run("suppresses completed-state readiness and stale blockers", func(t *testing.T) {
		status := "completed"

		output := MigrationStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{
				Status:          &status,
				DisplayMessage:  "Migration completed successfully",
				ReadyForCutover: false,
				CutoverBlockers: []string{"Migration already completed"},
			},
		})

		assert.Equal(t, `Cutover
  ✓ Completed
  Migration completed successfully
`, output)
	})

	t.Run("shows only cutover readiness for terminated migrations", func(t *testing.T) {
		status := "terminated"

		output := MigrationStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{
				Status:          &status,
				DisplayMessage:  "Migration terminated",
				ReadyForCutover: false,
				CutoverBlockers: []string{"Migration terminated"},
			},
		})

		assert.Equal(t, `Cutover
  ✗ Not ready for cutover
`, output)
	})

	t.Run("shows only cutover readiness for created migrations", func(t *testing.T) {
		status := "created"

		output := MigrationStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{Status: &status},
		})

		assert.Equal(t, `Cutover
  ✗ Not ready for cutover
`, output)
	})

	t.Run("shows only target availability for aborted targets", func(t *testing.T) {
		status := "aborted"

		output := MigrationStatus(elmapi.MigrationDetail{
			TargetState: &elmapi.TargetState{Status: &status},
		})

		assert.Equal(t, `Target
  ✓ Target available
`, output)
	})

	t.Run("hides target progress before a migration starts", func(t *testing.T) {
		created := "created"
		inProgress := "in_progress"

		output := MigrationStatus(elmapi.MigrationDetail{
			Migration:   &elmapi.MigrationSummary{Status: &created},
			TargetState: &elmapi.TargetState{Status: &inProgress},
		})

		assert.Contains(t, output, "○ Created")
		assert.Contains(t, output, "Target")
		assert.Contains(t, output, "✓ Available")
		assert.NotContains(t, output, "\nTarget\n")
		assert.NotContains(t, output, "In progress")
	})

	t.Run("preserves distinct repository phase and status", func(t *testing.T) {
		status := "backfilling"
		phase := "backfill"

		output := CutoverStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{
				Status:          &status,
				ReadyForCutover: false,
				Repositories: []elmapi.CombinedRepositoryState{{
					RepositoryNWO: "acme/web",
					Phase:         &phase,
					DisplayStatus: "In progress",
				}},
			},
		})

		assert.Contains(t, output, "acme/web · Backfill · In progress")
		assert.Contains(t, output, "✗ Not ready for cutover")
	})
}

func TestProgressBar(t *testing.T) {
	t.Run("renders proportional progress", func(t *testing.T) {
		styles := theme.New()
		assert.Equal(t,
			styles.ProgressBarFill.Render("━━━━━━━━")+styles.ProgressBarTrack.Render("━━"),
			ProgressBar(8, 10, 10),
		)
	})

	t.Run("clamps values to the bar bounds", func(t *testing.T) {
		styles := theme.New()
		assert.Equal(t, styles.ProgressBarTrack.Render("━━━━"), ProgressBar(-1, 10, 4))
		assert.Equal(t, styles.ProgressBarFill.Render("━━━━"), ProgressBar(12, 10, 4))
	})

	t.Run("renders an empty bar without a total", func(t *testing.T) {
		assert.Equal(t, theme.New().ProgressBarTrack.Render("━━━━"), ProgressBar(4, 0, 4))
	})
}

func TestProgressLine(t *testing.T) {
	t.Run("renders zero work as not started", func(t *testing.T) {
		output := progressLine("Backfill", 0, 0, 0)

		assert.Contains(t, output, "○ Not started")
		assert.NotContains(t, output, "0 / 0")
		assert.NotContains(t, output, "no failures")
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

		assert.Contains(t, output, "Migrations (1)")
		assert.Contains(t, output, "mig-1")
		assert.NotContains(t, output, "No migrations available.")
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
	t.Run("human output removes leading blank lines and adds one trailing blank line", func(t *testing.T) {
		var output bytes.Buffer

		require.NoError(t, Write(&output, "\n\nContent\n"))

		assert.Equal(t, "Content\n\n", output.String())
	})

	t.Run("success confirmation uses the semantic success glyph", func(t *testing.T) {
		assert.Equal(t, "✓ Operation completed.", Success("Operation completed."))
	})

	t.Run("warning uses the semantic warning glyph", func(t *testing.T) {
		assert.Equal(t, "! Needs attention.", Warning("Needs attention."))
	})

	t.Run("fields align labels and skip empty values", func(t *testing.T) {
		assert.Equal(t, "  ID      42\n  Status  ready", Fields(
			Field{Label: "ID", Value: "42"},
			Field{Label: "Ignored"},
			Field{Label: "Status", Value: "ready"},
		))
	})

	t.Run("success confirmation colors only the checkmark", func(t *testing.T) {
		previousProfile := lipgloss.ColorProfile()
		lipgloss.SetColorProfile(termenv.ANSI256)
		t.Cleanup(func() {
			lipgloss.SetColorProfile(previousProfile)
		})

		output := Success("Operation completed.")

		assert.Equal(t, theme.New().Success.Render("✓")+" Operation completed.", output)
		assert.NotContains(t, output, theme.New().Success.Render("Operation completed."))
	})

	t.Run("human output surfaces writer errors", func(t *testing.T) {
		assert.Error(t, Write(failWriter{}, "output"))
	})

	t.Run("JSON surfaces writer errors", func(t *testing.T) {
		assert.Error(t, WriteRawJSON(failWriter{}, []byte(`{"ok":true}`)))
	})

	t.Run("JSON is indented and ends with one newline", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, WriteRawJSON(&output, []byte(` { "ok": true, "nested": {"value": 1} } `)))
		assert.Equal(t, "{\n  \"ok\": true,\n  \"nested\": {\n    \"value\": 1\n  }\n}\n", output.String()) //nolint:testifylint // Exact whitespace is the behavior under test.
	})

	t.Run("invalid JSON is rejected", func(t *testing.T) {
		assert.ErrorContains(t, WriteRawJSON(&bytes.Buffer{}, []byte(`{"ok":`)), "formatting JSON response")
	})
}
