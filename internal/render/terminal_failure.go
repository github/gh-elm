package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/theme"
)

// nowFunc is the clock used to render relative failure times. It is a variable
// so tests can pin it; widening the exported renderers to accept a clock would
// churn every call site for one line of output.
var nowFunc = time.Now

// genericFailureSummary is shown when the server recorded a failure but no
// usable text, so a failed migration never renders as though nothing happened.
const genericFailureSummary = "Migration failed"

// TerminalFailureFor returns the cause to display for a migration, or nil when
// none was recorded.
//
// CombinedState wins because the server populates it only for a genuine
// failure, making it the user-facing view. TargetState is the fallback: it is
// the destination's faithful report, and it can be set when the combined status
// describes something other than a failure, so preferring it would surface a
// cause on migrations the server does not consider failed.
func TerminalFailureFor(detail elmapi.MigrationDetail) *elmapi.TerminalFailure {
	if combined := detail.CombinedState; combined != nil && combined.TerminalFailure != nil {
		return combined.TerminalFailure
	}
	if target := detail.TargetState; target != nil && target.TerminalFailure != nil {
		return target.TerminalFailure
	}
	return nil
}

// TerminalFailureSummary returns the sentence describing a failure, falling
// back to the code and then to a generic sentence.
//
// The summary is never derived from the code beyond that fallback: the server
// authors the text, so deriving it here would silently go stale as new codes
// are added.
func TerminalFailureSummary(failure *elmapi.TerminalFailure) string {
	if failure == nil {
		return ""
	}
	if summary := strings.TrimSpace(failure.Summary); summary != "" {
		return summary
	}
	if code := strings.TrimSpace(failure.Code); code != "" {
		return friendlyValue(code)
	}
	return genericFailureSummary
}

// renderTerminalFailure renders the failure section for a migration status
// document, or "" when no cause was recorded.
func renderTerminalFailure(failure *elmapi.TerminalFailure) string {
	if failure == nil {
		return ""
	}

	styles := theme.New()
	lines := []string{
		bullet(styles.Failure.Render("✗"), styles.Failure.Bold(true).Render(TerminalFailureSummary(failure))),
	}
	// The code is rendered verbatim rather than prettified: it is a stable
	// contract value, so keeping it greppable and quotable in a support
	// escalation is worth more than title casing.
	if code := strings.TrimSpace(failure.Code); code != "" {
		lines = append(lines, field("Code", styles.Bold.Render(code)))
	}
	if occurred := formatOccurredAt(failure.OccurredAt); occurred != "" {
		lines = append(lines, field("Occurred", occurred))
	}
	return renderSection("Failure", lines...)
}

// formatOccurredAt renders a failure timestamp as "<relative> (<absolute>)",
// or "" when no time was recorded.
//
// An absent timestamp yields no line at all rather than an em dash, which would
// imply the failure happened at an unknown time instead of simply not having
// been stamped.
func formatOccurredAt(occurredAt *string) string {
	if occurredAt == nil || strings.TrimSpace(*occurredAt) == "" {
		return ""
	}

	absolute := strings.TrimSpace(*occurredAt)
	parsed, err := time.Parse(time.RFC3339, absolute)
	if err != nil {
		// An unparseable timestamp is still information; show it as sent
		// rather than dropping the line.
		return absolute
	}

	styles := theme.New()
	return relativeTime(parsed, nowFunc()) + styles.Muted.Render(" ("+absolute+")")
}

// relativeTime renders how long before now a moment was, in the same units as
// the watch timeline.
func relativeTime(moment, now time.Time) string {
	elapsed := now.Sub(moment)
	if elapsed < 0 {
		// Clock skew between the appliance and this machine; "just now" beats
		// rendering a negative age.
		return "just now"
	}
	switch {
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds ago", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh %dm ago", int(elapsed.Hours()), int(elapsed.Minutes())%60)
	default:
		return fmt.Sprintf("%dd ago", int(elapsed.Hours())/24)
	}
}
