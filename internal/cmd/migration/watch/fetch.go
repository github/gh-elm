package watch

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/github/gh-elm/internal/elmapi"
)

// statusMsg is the message sent when a fetch completes.
type statusMsg struct {
	detail    *elmapi.MigrationDetail
	err       error
	fetchedAt time.Time
}

// tickMsg triggers a new fetch.
type tickMsg time.Time

// fetchTimeout computes a context timeout from the refresh interval.
// Uses interval * 5 (generous for slow networks), capped at 30s.
func fetchTimeout(interval time.Duration) time.Duration {
	const maxTimeout = 30 * time.Second
	const minTimeout = time.Second
	return max(min(interval*5, maxTimeout), minTimeout)
}

// fetchStatus fetches the migration status document. Unlike the TWIRP CLI, the
// REST API exposes a single GET that already folds in cutover/combined state, so
// there is no separate cutover fetch. This is a synchronous function called from
// within a tea.Cmd.
func fetchStatus(client *elmapi.Client, migrationID string, interval time.Duration) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout(interval))
	defer cancel()

	detail, err := client.GetMigrationDetail(ctx, migrationID)
	if err != nil {
		return statusMsg{err: err, fetchedAt: time.Now()}
	}

	return statusMsg{detail: detail, fetchedAt: time.Now()}
}

// fetchStatusCmd returns a tea.Cmd that fetches status.
func fetchStatusCmd(client *elmapi.Client, migrationID string, interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		return fetchStatus(client, migrationID, interval)
	}
}

// tickCmd returns a tea.Cmd that waits for the interval then sends a tick.
func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
