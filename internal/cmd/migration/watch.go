package migration

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/cmd/migration/watch"
	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
)

// annotateAuthError turns a 401/403 from the source API into an actionable
// message. Auth failures are commonly caused by a stale GH_SOURCE_HOST or
// GH_SOURCE_TOKEN overriding the configured values, so we call that out.
func annotateAuthError(err error, sourceURL string) error {
	var httpErr *elmapi.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("authentication failed (HTTP %d) for source %s: %s; "+
			"check the source token with `gh elm configure --show`. Note the %s and %s environment variables override stored config",
			httpErr.StatusCode, sourceURL, httpErr.Message, config.EnvSourceURL, config.EnvSourceToken)
	}
	return err
}

// writeCutoverStatus renders the cutover-readiness view derived from a
// migration's combined state.
func writeCutoverStatus(w io.Writer, detail *elmapi.MigrationDetail) error {
	cs := detail.CombinedState
	if cs == nil {
		_, err := fmt.Fprintln(w, "No combined state reported for this migration yet.")
		return err
	}

	fmt.Fprintf(w, "Status:            %s\n", deref(cs.Status))
	if cs.DisplayMessage != "" {
		fmt.Fprintf(w, "Message:           %s\n", cs.DisplayMessage)
	}
	fmt.Fprintf(w, "Ready for cutover: %t\n", cs.ReadyForCutover)

	if len(cs.CutoverBlockers) > 0 {
		fmt.Fprintln(w, "Blockers:")
		for _, b := range cs.CutoverBlockers {
			fmt.Fprintf(w, "  - %s\n", b)
		}
	}

	for _, r := range cs.Repositories {
		phase := deref(r.Phase)
		fmt.Fprintf(w, "Repository %s: %s", r.RepositoryNWO, r.DisplayStatus)
		if phase != "" {
			fmt.Fprintf(w, " (phase: %s)", phase)
		}
		fmt.Fprintln(w)
	}

	return nil
}

// defaultWatchInterval is the default refresh interval for watch mode.
const defaultWatchInterval = 2 * time.Second

// newWatchCmd builds `gh elm migration watch`.
func newWatchCmd() *cobra.Command {
	var (
		migrationID string
		intervalStr string
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch migration progress with a live-updating display",
		Long: "Display a live-updating phased timeline of migration progress, including\n" +
			"export, backfill, and cutover status.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid interval %q: %w", intervalStr, err)
			}
			if interval <= 0 {
				return fmt.Errorf("interval must be positive, got %s", interval)
			}

			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			return runWatchInterval(cmd, client, srcURL, migrationID, interval)
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to watch (required).")
	cmd.Flags().StringVarP(&intervalStr, "interval", "i", "2s", "Refresh interval (e.g. 2s, 5s, 1m).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// runWatch watches a migration at the default interval. Used by the --watch flag
// on create/start/cutover.
func runWatch(cmd *cobra.Command, client *elmapi.Client, sourceURL, migrationID string) error {
	return runWatchInterval(cmd, client, sourceURL, migrationID, defaultWatchInterval)
}

// runWatchInterval starts the bubbletea watch program for the given migration.
// sourceURL is currently unused (fetch errors surface in the watch footer rather
// than being annotated) but is kept for signature symmetry with the callers.
func runWatchInterval(cmd *cobra.Command, client *elmapi.Client, _ /* sourceURL */, migrationID string, interval time.Duration) error {
	model := watch.New(migrationID, interval, client)
	p := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("watch display error: %w", err)
	}
	return nil
}

// deref returns the value of a *string, or "" when nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
