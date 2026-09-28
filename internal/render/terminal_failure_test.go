package render

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-elm/internal/elmapi"
)

// pinNow freezes the clock used for relative failure times so assertions do not
// drift with wall time.
func pinNow(t *testing.T, now time.Time) {
	t.Helper()
	previous := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = previous })
}

func failure(code, summary string, occurredAt *string) *elmapi.TerminalFailure {
	return &elmapi.TerminalFailure{Code: code, Summary: summary, OccurredAt: occurredAt}
}

func TestTerminalFailureFor(t *testing.T) {
	t.Run("prefers the combined state cause", func(t *testing.T) {
		detail := elmapi.MigrationDetail{
			TargetState:   &elmapi.TargetState{TerminalFailure: failure("critical_resource", "Target view", nil)},
			CombinedState: &elmapi.CombinedState{TerminalFailure: failure("repository_policy", "Combined view", nil)},
		}

		resolved := TerminalFailureFor(detail)

		require.NotNil(t, resolved)
		assert.Equal(t, "repository_policy", resolved.Code)
	})

	t.Run("falls back to the target state cause", func(t *testing.T) {
		detail := elmapi.MigrationDetail{
			TargetState:   &elmapi.TargetState{TerminalFailure: failure("critical_resource", "Target view", nil)},
			CombinedState: &elmapi.CombinedState{},
		}

		resolved := TerminalFailureFor(detail)

		require.NotNil(t, resolved)
		assert.Equal(t, "critical_resource", resolved.Code)
	})

	t.Run("returns nil when nothing failed", func(t *testing.T) {
		assert.Nil(t, TerminalFailureFor(elmapi.MigrationDetail{
			TargetState:   &elmapi.TargetState{},
			CombinedState: &elmapi.CombinedState{},
		}))
	})

	t.Run("returns nil for an empty document", func(t *testing.T) {
		assert.Nil(t, TerminalFailureFor(elmapi.MigrationDetail{}))
	})
}

func TestTerminalFailureSummary(t *testing.T) {
	t.Run("uses the server summary", func(t *testing.T) {
		assert.Equal(t, "Policy blocked it.", TerminalFailureSummary(failure("repository_policy", "Policy blocked it.", nil)))
	})

	t.Run("falls back to the code when no summary is sent", func(t *testing.T) {
		assert.Equal(t, "Repository policy", TerminalFailureSummary(failure("repository_policy", "", nil)))
	})

	t.Run("falls back to a generic sentence when the failure is empty", func(t *testing.T) {
		assert.Equal(t, "Migration failed", TerminalFailureSummary(failure("", "  ", nil)))
	})

	t.Run("returns an empty string for no failure", func(t *testing.T) {
		assert.Empty(t, TerminalFailureSummary(nil))
	})
}

func TestRenderTerminalFailure(t *testing.T) {
	t.Run("renders the summary, code, and time", func(t *testing.T) {
		pinNow(t, time.Date(2026, 9, 4, 13, 0, 37, 0, time.UTC))
		occurredAt := "2026-09-04T12:58:37Z"

		output := renderTerminalFailure(failure("repository_policy", "Policy blocked it.", &occurredAt))

		assert.Contains(t, output, "Failure")
		assert.Contains(t, output, "Policy blocked it.")
		assert.Contains(t, output, "repository_policy")
		assert.Contains(t, output, "2m ago (2026-09-04T12:58:37Z)")
	})

	t.Run("renders a code it does not recognize", func(t *testing.T) {
		output := renderTerminalFailure(failure("some_future_code", "Something new went wrong.", nil))

		assert.Contains(t, output, "Something new went wrong.")
		assert.Contains(t, output, "some_future_code")
	})

	t.Run("omits the occurred line when no time was recorded", func(t *testing.T) {
		output := renderTerminalFailure(failure("repository_policy", "Policy blocked it.", nil))

		assert.Contains(t, output, "Policy blocked it.")
		assert.NotContains(t, output, "Occurred")
	})

	t.Run("renders nothing when no failure was recorded", func(t *testing.T) {
		assert.Empty(t, renderTerminalFailure(nil))
	})
}

func TestFormatOccurredAt(t *testing.T) {
	t.Run("returns an unparseable timestamp unchanged", func(t *testing.T) {
		malformed := "not-a-timestamp"

		assert.Equal(t, "not-a-timestamp", formatOccurredAt(&malformed))
	})

	t.Run("returns an empty string for a nil timestamp", func(t *testing.T) {
		assert.Empty(t, formatOccurredAt(nil))
	})

	t.Run("returns an empty string for a blank timestamp", func(t *testing.T) {
		blank := "   "

		assert.Empty(t, formatOccurredAt(&blank))
	})
}

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	t.Run("renders seconds", func(t *testing.T) {
		assert.Equal(t, "30s ago", relativeTime(now.Add(-30*time.Second), now))
	})

	t.Run("renders minutes", func(t *testing.T) {
		assert.Equal(t, "5m ago", relativeTime(now.Add(-5*time.Minute), now))
	})

	t.Run("renders hours and minutes", func(t *testing.T) {
		assert.Equal(t, "3h 5m ago", relativeTime(now.Add(-(3*time.Hour+5*time.Minute)), now))
	})

	t.Run("renders days", func(t *testing.T) {
		assert.Equal(t, "2d ago", relativeTime(now.Add(-50*time.Hour), now))
	})

	t.Run("renders a future timestamp as just now", func(t *testing.T) {
		assert.Equal(t, "just now", relativeTime(now.Add(time.Minute), now))
	})
}

func TestMigrationStatusTerminalFailure(t *testing.T) {
	failedStatus := "failed"

	t.Run("renders the failure section above target state", func(t *testing.T) {
		pinNow(t, time.Date(2026, 9, 4, 13, 0, 37, 0, time.UTC))
		occurredAt := "2026-09-04T12:58:37Z"
		output := MigrationStatus(elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "mig-1", Status: &failedStatus},
			TargetState: &elmapi.TargetState{
				Status:          &failedStatus,
				TerminalFailure: failure("repository_policy", "Policy blocked it.", &occurredAt),
			},
		})

		require.Contains(t, output, "Failure")
		assert.Less(t, strings.Index(output, "Failure"), strings.Index(output, "Target"))
		assert.Contains(t, output, "Policy blocked it.")
	})

	t.Run("does not repeat the summary as the combined display message", func(t *testing.T) {
		summary := "Policy blocked it."
		output := MigrationStatus(elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "mig-1", Status: &failedStatus},
			CombinedState: &elmapi.CombinedState{
				Status:          &failedStatus,
				DisplayMessage:  summary,
				TerminalFailure: failure("repository_policy", summary, nil),
			},
		})

		assert.Equal(t, 1, strings.Count(output, summary),
			"the authored summary should render once, in the failure section")
	})

	t.Run("still renders a display message that differs from the summary", func(t *testing.T) {
		output := MigrationStatus(elmapi.MigrationDetail{
			Migration: &elmapi.MigrationSummary{MigrationID: "mig-1", Status: &failedStatus},
			CombinedState: &elmapi.CombinedState{
				Status:          &failedStatus,
				DisplayMessage:  "Failed: 2 resources failed",
				TerminalFailure: failure("repository_policy", "Policy blocked it.", nil),
			},
		})

		assert.Contains(t, output, "Policy blocked it.")
		assert.Contains(t, output, "Failed: 2 resources failed")
	})

	t.Run("renders no failure section for a healthy migration", func(t *testing.T) {
		inProgress := "in_progress"
		output := MigrationStatus(elmapi.MigrationDetail{
			Migration:   &elmapi.MigrationSummary{MigrationID: "mig-1", Status: &inProgress},
			TargetState: &elmapi.TargetState{Status: &inProgress},
		})

		assert.NotContains(t, output, "Failure")
	})
}

func TestCutoverStatusTerminalFailure(t *testing.T) {
	t.Run("does not repeat the summary as the display message", func(t *testing.T) {
		failedStatus := "failed"
		summary := "Policy blocked it."
		output := CutoverStatus(elmapi.MigrationDetail{
			CombinedState: &elmapi.CombinedState{
				Status:          &failedStatus,
				DisplayMessage:  summary,
				TerminalFailure: failure("repository_policy", summary, nil),
			},
		})

		assert.NotContains(t, output, summary)
	})
}
