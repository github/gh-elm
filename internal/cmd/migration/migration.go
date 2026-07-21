// Package migration implements the `gh elm migration` command group, which
// drives the migration lifecycle against the source (GHES) REST API
// (/enterprise/live-migrations; see github/github
// app/api/description/operations/enterprise-admin/live-migration-*.yaml).
package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
)

// NewCommand builds the `gh elm migration` command group. Subcommands mirror the
// `elm migration *` group in elm-exporter (cmd/elm/cmd/migration.go), reimplemented
// against the GHES REST API.
func NewCommand() *cobra.Command {
	migrationCmd := &cobra.Command{
		Use:   "migration",
		Short: "Manage migrations via the GHES REST API",
		Long: "Commands for creating, starting, monitoring, and controlling Enterprise Live\n" +
			"Migrations through the GitHub Enterprise Server REST API.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	migrationCmd.AddCommand(
		newCreateCmd(),
		newStartCmd(),
		newStatusCmd(),
		newListCmd(),
		newCancelCmd(),
		newCutoverCmd(),
		newCutoverStatusCmd(),
		newRevertCutoverCmd(),
		newPauseCmd(),
		newResumeCmd(),
		newWatchCmd(),
	)

	return migrationCmd
}

// sourceClient resolves the source (GHES) endpoint (flags override env override
// stored config) and returns a ready client plus the resolved base URL for error
// messages.
func sourceClient(sourceURL, sourceToken string) (*elmapi.Client, string, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, "", err
	}
	ep, err := resolver.Source(sourceURL, sourceToken)
	if err != nil {
		return nil, "", err
	}
	if ep.URL == "" {
		return nil, "", fmt.Errorf("no source URL configured; run `gh elm configure`, set %s, or pass --source-url", config.EnvSourceURL)
	}
	if ep.Token == "" {
		return nil, "", fmt.Errorf("no source token configured; run `gh elm configure`, set %s, or pass --source-token", config.EnvSourceToken)
	}
	return elmapi.NewClient(ep.URL, ep.Token), ep.URL, nil
}

// sourceFlags registers the shared --source-url/--source-token overrides on a
// command and returns pointers to their backing values.
func sourceFlags(cmd *cobra.Command) {
	sourceURL := new(string)
	sourceToken := new(string)
	cmd.Flags().StringVar(sourceURL, "source-url", "", "Override the source (GHES) API base URL.")
	cmd.Flags().StringVar(sourceToken, "source-token", "", "Override the source (GHES) API token.")
}

// resolveTargetEndpoint returns the configured target (GHEC) API base URL
// (GH_TARGET_HOST / stored config), scheme-normalized to https. It exists ONLY
// to satisfy an API defect: the create endpoint requires a target_api_endpoint
// even though these commands otherwise never call the target API, so there is no
// dedicated target-api flag. Returns "" when no target host is configured.
func resolveTargetEndpoint() (string, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return "", err
	}
	ep, err := resolver.Target("", "")
	if err != nil {
		return "", err
	}
	return ep.URL, nil
}

// newCreateCmd builds `gh elm migration create`.
func newCreateCmd() *cobra.Command {
	var (
		sourceOrg        string
		sourceRepo       string
		targetOrg        string
		targetRepo       string
		targetVisibility string
		start            bool
		watch            bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new migration",
		Long: "Create a new Enterprise Live Migration to prepare for repository export and\n" +
			"import. The migration is created in a `created` state; pass --start to launch\n" +
			"it immediately.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			visibility, err := resolveVisibility(targetVisibility)
			if err != nil {
				return err
			}
			if watch && !start {
				return errors.New("--watch requires --start: a migration must be started before it can be watched")
			}

			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}

			// WORKAROUND (API defect): the create endpoint requires a
			// target_api_endpoint, even though every other migration command here
			// only talks to the source (GHES) API and this CLI has no target-api
			// flag. Until the API stops requiring it, derive the endpoint from the
			// configured target host (GH_TARGET_HOST / stored config), e.g.
			// GH_TARGET_HOST=api.staffship-01.ghe.com -> https://api.staffship-01.ghe.com.
			targetAPI, err := resolveTargetEndpoint()
			if err != nil {
				return err
			}
			if targetAPI == "" {
				return fmt.Errorf("the create API requires a target endpoint; set %s (for example api.staffship-01.ghe.com) or run `gh elm configure`", config.EnvTargetURL)
			}

			req := elmapi.CreateMigrationRequest{
				SourceOrganizationLogin: sourceOrg,
				SourceRepositoryName:    sourceRepo,
				TargetOrganizationLogin: targetOrg,
				TargetRepositoryName:    targetRepo,
				TargetAPIEndpoint:       targetAPI,
				// WORKAROUND (API defect): the create endpoint requires a
				// non-empty pat_name, but migration credentials are supplied by
				// the system rather than this CLI, so there is nothing meaningful
				// to send. Stub it with a sentinel until the API stops requiring it.
				PATName:          "BOGON",
				TargetVisibility: visibility,
			}

			resp, err := client.CreateMigration(cmd.Context(), req)
			if err != nil {
				return annotateAuthError(err, srcURL)
			}

			if !start {
				return writeJSON(cmd.OutOrStdout(), resp)
			}

			if err := client.StartMigration(cmd.Context(), resp.MigrationID); err != nil {
				return fmt.Errorf("migration %s created but failed to start: %w", resp.MigrationID, annotateAuthError(err, srcURL))
			}

			if watch {
				return runWatch(cmd, client, srcURL, resp.MigrationID)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Migration %s created and started.\n", resp.MigrationID)
			return nil
		},
	}

	cmd.Flags().StringVar(&sourceOrg, "source-org", "", "Source organization login (required).")
	cmd.Flags().StringVar(&sourceRepo, "source-repo", "", "Source repository name (required).")
	cmd.Flags().StringVar(&targetOrg, "target-org", "", "Target organization login (required).")
	cmd.Flags().StringVar(&targetRepo, "target-repo", "", "Target repository name (required).")
	cmd.Flags().StringVar(&targetVisibility, "target-visibility", "internal", "Target repository visibility (private or internal).")
	cmd.Flags().BoolVar(&start, "start", false, "Automatically start the migration after creating it.")
	cmd.Flags().BoolVar(&watch, "watch", false, "After creating and starting, enter live watch mode (requires --start).")
	sourceFlags(cmd)
	for _, f := range []string{"source-org", "source-repo", "target-org", "target-repo"} {
		_ = cmd.MarkFlagRequired(f)
	}

	return cmd
}

// newStartCmd builds `gh elm migration start`.
func newStartCmd() *cobra.Command {
	var (
		migrationID string
		watch       bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a previously created migration",
		Long:  "Start a created migration, launching backfill and enabling live updates.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			if err := client.StartMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, srcURL)
			}
			if watch {
				return runWatch(cmd, client, srcURL, migrationID)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %s started.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&watch, "watch", false, "After starting, enter live watch mode.")
	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to start (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newStatusCmd builds `gh elm migration status`.
func newStatusCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get the status and details of a migration",
		Long: "Retrieve combined status, progress, cutover readiness, expiration, and timing\n" +
			"for a migration. Prints the API's raw JSON response.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			raw, err := client.GetMigration(cmd.Context(), migrationID)
			if err != nil {
				return annotateAuthError(err, srcURL)
			}
			return writeRaw(cmd.OutOrStdout(), raw)
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to get status for (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newListCmd builds `gh elm migration list`.
func newListCmd() *cobra.Command {
	var (
		status   string
		pageSize int
		after    string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List migrations (defaults to in-progress only)",
		Long: "List migrations, with optional filtering by status and cursor-based pagination.\n" +
			"Use --status to filter (created, queued, in_progress, paused, completed, failed,\n" +
			"terminated) or --status=all to list migrations in every state. Prints the API's\n" +
			"raw JSON response.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if status != "" {
				if err := validateStatus(status); err != nil {
					return err
				}
			}
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			raw, err := client.ListMigrations(cmd.Context(), elmapi.ListMigrationsOptions{
				Status:   status,
				PageSize: pageSize,
				After:    after,
			})
			if err != nil {
				return annotateAuthError(err, srcURL)
			}
			return writeRaw(cmd.OutOrStdout(), raw)
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter by status (all, created, queued, in_progress, paused, completed, failed, terminated). Defaults to in_progress.")
	cmd.Flags().IntVar(&pageSize, "page-size", 0, "Number of migrations per page (1-100).")
	cmd.Flags().StringVar(&after, "after", "", "Cursor for pagination (from next_cursor in a previous response).")
	sourceFlags(cmd)

	return cmd
}

// newCancelCmd builds `gh elm migration cancel`.
func newCancelCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel and terminate a migration",
		Long: "Terminate a migration: it is cancelled locally, aborted on the ELM backend, and\n" +
			"its work items are removed. This is a terminal action with no recovery.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			if err := client.CancelMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, srcURL)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %s cancelled.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to cancel (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newCutoverCmd builds `gh elm migration cutover-to-destination`.
func newCutoverCmd() *cobra.Command {
	var (
		migrationID string
		force       bool
		watch       bool
	)

	cmd := &cobra.Command{
		Use:   "cutover-to-destination",
		Short: "Initiate a cutover to the destination for a migration",
		Long: "Initiate a cutover to the destination, archiving the source repository and\n" +
			"draining remaining changes. Cutover is asynchronous; query status to observe\n" +
			"progress. Use --force to bypass the readiness check.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			if err := client.Cutover(cmd.Context(), migrationID, force); err != nil {
				return annotateAuthError(err, srcURL)
			}
			if watch {
				return runWatch(cmd, client, srcURL, migrationID)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cutover initiated for migration %s.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Bypass the cutover readiness check and proceed immediately.")
	cmd.Flags().BoolVar(&watch, "watch", false, "After triggering cutover, enter live watch mode.")
	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to initiate cutover for (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newCutoverStatusCmd builds `gh elm migration cutover-status`. The REST API has
// no dedicated cutover-status endpoint; cutover readiness is derived from the
// combined_state of the GET status document.
func newCutoverStatusCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "cutover-status",
		Short: "Get the cutover status and progress for a migration",
		Long: "Report cutover readiness for a migration, including whether it is ready for\n" +
			"cutover and any outstanding blockers. Derived from the migration's combined\n" +
			"state (there is no dedicated cutover-status REST endpoint).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			detail, err := client.GetMigrationDetail(cmd.Context(), migrationID)
			if err != nil {
				return annotateAuthError(err, srcURL)
			}
			return writeCutoverStatus(cmd.OutOrStdout(), detail)
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to get cutover status for (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newRevertCutoverCmd builds `gh elm migration revert-cutover`.
func newRevertCutoverCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "revert-cutover",
		Short: "Revert the effects of a cutover so the source repository can be migrated again",
		Long: "Revert the effects of a cutover, unarchiving the source repository and\n" +
			"terminating any cutover or migration still in progress so the source repository\n" +
			"can be migrated again. Prints the API's raw JSON response.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			resp, err := client.RevertCutover(cmd.Context(), migrationID)
			if err != nil {
				return annotateAuthError(err, srcURL)
			}
			return writeJSON(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to revert cutover for (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newPauseCmd builds `gh elm migration pause`.
func newPauseCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a running migration",
		Long: "Pause source-load work (backfill and Git synchronization) for an active\n" +
			"migration while live event collection continues. Idempotent for an already\n" +
			"paused migration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			if err := client.PauseMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, srcURL)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %s paused.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to pause (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newResumeCmd builds `gh elm migration resume`.
func newResumeCmd() *cobra.Command {
	var migrationID string

	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a paused migration",
		Long:  "Resume a paused migration; it is re-queued and resumes backfill and live updates.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, srcURL, err := sourceClient(*sourceURLFlag(cmd), *sourceTokenFlag(cmd))
			if err != nil {
				return err
			}
			if err := client.ResumeMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, srcURL)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %s resumed.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&migrationID, "migration-id", "m", "", "Migration ID (UUID) to resume (required).")
	sourceFlags(cmd)
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// sourceURLFlag / sourceTokenFlag read the shared source overrides back off a
// command's flag set. They return a pointer to the flag's current value.
func sourceURLFlag(cmd *cobra.Command) *string {
	v, _ := cmd.Flags().GetString("source-url")
	return &v
}

func sourceTokenFlag(cmd *cobra.Command) *string {
	v, _ := cmd.Flags().GetString("source-token")
	return &v
}

// resolveVisibility validates the --target-visibility flag. The REST API accepts
// only private or internal (a public source repository must be migrated as one
// of these).
func resolveVisibility(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", elmapi.VisibilityInternal:
		return elmapi.VisibilityInternal, nil
	case elmapi.VisibilityPrivate:
		return elmapi.VisibilityPrivate, nil
	default:
		return "", fmt.Errorf("invalid --target-visibility %q: must be private or internal", s)
	}
}

// validateStatus checks a --status filter value against the accepted set.
func validateStatus(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case elmapi.StatusAll, elmapi.StatusCreated, elmapi.StatusQueued, elmapi.StatusInProgress,
		elmapi.StatusPaused, elmapi.StatusCompleted, elmapi.StatusFailed, elmapi.StatusTerminated:
		return nil
	default:
		return fmt.Errorf("invalid --status %q: must be one of all, created, queued, in_progress, paused, completed, failed, terminated", s)
	}
}

// writeRaw echoes the API's raw JSON response verbatim, followed by a newline.
func writeRaw(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

// writeJSON marshals v as indented JSON followed by a newline.
func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding response: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
