package target

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/elmapi"
)

// newMigrationCmd builds the `gh elm target migration` command group, which
// manages a migration record directly on the target (GHEC/Proxima) side:
// list, create, check status, pause, resume, and abort. Most engineers create
// migrations indirectly via `gh elm migration create` (the source/GHES API,
// which drives the target record for them); this group exists for debugging
// and for advanced or customer workflows that operate on the target migration
// record directly.
func newMigrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migration",
		Short: "List, create, and control migrations on the target",
		Long: "Manage a migration record directly on the target (GHEC/Proxima) side: list,\n" +
			"create, check status, pause, resume, and abort a migration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newMigrationListCmd())
	cmd.AddCommand(newMigrationCreateCmd())
	cmd.AddCommand(newMigrationStatusCmd())
	cmd.AddCommand(newMigrationPauseCmd())
	cmd.AddCommand(newMigrationResumeCmd())
	cmd.AddCommand(newMigrationAbortCmd())

	return cmd
}

// newMigrationListCmd builds `gh elm target migration list`, which lists
// migration records on the target. GET /enterprise/migration/list.
func newMigrationListCmd() *cobra.Command {
	var (
		statusFlag  string
		maxResults  int
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List migrations on the target",
		Long: "List migration records on the target (GHEC/Proxima) side. Use --status to\n" +
			"filter client-side (the API does not support server-side status filtering).\n" +
			"Repository progress is not populated here; use `status` for that.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := resolveTargetMigrationStatus(statusFlag)
			if err != nil {
				return err
			}

			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			printed := 0
			for m, err := range client.IterTargetMigrations(cmd.Context(), elmapi.ListTargetMigrationsOptions{}) {
				if err != nil {
					return annotateAuthError(err, targetURLResolved)
				}
				if status != "" && m.Status != status {
					continue
				}
				if maxResults > 0 && printed >= maxResults {
					break
				}
				printed++
				if asJSON {
					if err := printMigrationJSON(out, m); err != nil {
						return err
					}
					continue
				}
				printMigration(out, m)
			}

			if !asJSON {
				fmt.Fprintln(out, migrationCountSummary(printed))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&statusFlag, "status", "", "Filter by status: in_progress, complete, failed, aborted, expired, or paused (default: all).")
	cmd.Flags().IntVar(&maxResults, "max-results", 0, "Maximum number of migrations to return (0 = all).")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output migrations as newline-delimited JSON.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")

	return cmd
}

// newMigrationCreateCmd builds `gh elm target migration create`, which creates
// a migration record on the target. POST /enterprise/migration/create.
func newMigrationCreateCmd() *cobra.Command {
	var (
		sourceURL    string
		repository   string
		description  string
		exporterGUID string
		asJSON       bool
		targetURL    string
		targetToken  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a migration on the target",
		Long: "Create a migration record directly on the target (GHEC/Proxima) side. This is\n" +
			"a lower-level call than `gh elm migration create` (which drives creation from\n" +
			"the source/GHES side); use it for debugging or advanced workflows. Only a\n" +
			"single repository is currently supported per migration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}

			req := elmapi.CreateTargetMigrationRequest{
				SourceURL:             sourceURL,
				Repositories:          []string{repository},
				Description:           description,
				ExporterMigrationGUID: exporterGUID,
			}
			raw, err := client.CreateTargetMigration(cmd.Context(), req)
			if err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			return renderReport(cmd.OutOrStdout(), raw, asJSON, printMigrationCreate)
		},
	}

	cmd.Flags().StringVar(&sourceURL, "source-url", "", "Source URL for the migration (required).")
	cmd.Flags().StringVarP(&repository, "repository", "R", "", "Repository to migrate, in owner/repo format (required).")
	cmd.Flags().StringVar(&description, "description", "", "Optional description of the migration.")
	cmd.Flags().StringVar(&exporterGUID, "exporter-migration-guid", "", "Optional exporter-minted UUID correlating this migration with exporter-side records.")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output the API's raw JSON response instead of human-readable text.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("source-url")
	_ = cmd.MarkFlagRequired("repository")

	return cmd
}

// newMigrationStatusCmd builds `gh elm target migration status`, which reports
// status and per-repository progress for one migration.
// GET /enterprise/migration/{id}/status.
func newMigrationStatusCmd() *cobra.Command {
	var (
		migrationID int64
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get the status of a migration on the target",
		Long: "Get status and per-repository progress for a migration from the target\n" +
			"(GHEC/Proxima) side.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			resp, err := client.GetTargetMigrationStatus(cmd.Context(), migrationID)
			if err != nil {
				return annotateAuthError(err, targetURLResolved)
			}

			if asJSON {
				return printMigrationJSON(cmd.OutOrStdout(), resp.Migration)
			}
			printMigration(cmd.OutOrStdout(), resp.Migration)
			return nil
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to get status for (required).")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output the migration as JSON.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newMigrationPauseCmd builds `gh elm target migration pause`, which pauses a
// migration on the target. POST /enterprise/migration/{id}/pause.
func newMigrationPauseCmd() *cobra.Command {
	var (
		migrationID int64
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a migration on the target",
		Long:  "Pause a migration directly on the target (GHEC/Proxima) side.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			if err := client.PauseTargetMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %d paused.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to pause (required).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newMigrationResumeCmd builds `gh elm target migration resume`, which resumes
// a paused migration on the target. POST /enterprise/migration/{id}/resume.
func newMigrationResumeCmd() *cobra.Command {
	var (
		migrationID int64
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a paused migration on the target",
		Long:  "Resume a paused migration directly on the target (GHEC/Proxima) side.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			if err := client.ResumeTargetMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %d resumed.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to resume (required).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// newMigrationAbortCmd builds `gh elm target migration abort`, which aborts a
// migration on the target. POST /enterprise/migration/{id}/abort.
func newMigrationAbortCmd() *cobra.Command {
	var (
		migrationID int64
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "abort",
		Short: "Abort a migration on the target",
		Long:  "Abort a migration directly on the target (GHEC/Proxima) side. This is a terminal action.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			if err := client.AbortTargetMigration(cmd.Context(), migrationID); err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Migration %d aborted.\n", migrationID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to abort (required).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")

	return cmd
}

// resolveTargetMigrationStatus maps the --status flag on
// `target migration list` to its wire value. An empty flag omits the filter.
func resolveTargetMigrationStatus(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "in_progress", "in-progress":
		return elmapi.TargetMigrationStatusInProgress, nil
	case "complete", "completed":
		return elmapi.TargetMigrationStatusComplete, nil
	case "failed":
		return elmapi.TargetMigrationStatusFailed, nil
	case "aborted":
		return elmapi.TargetMigrationStatusAborted, nil
	case "expired":
		return elmapi.TargetMigrationStatusExpired, nil
	case "paused":
		return elmapi.TargetMigrationStatusPaused, nil
	default:
		return "", fmt.Errorf("invalid --status %q: must be in_progress, complete, failed, aborted, expired, or paused", s)
	}
}

func printMigration(w io.Writer, m elmapi.TargetMigration) {
	fmt.Fprintf(w, "Migration ID: %s\n", m.MigrationID)
	fmt.Fprintf(w, "Status:       %s\n", friendlyEnum(m.Status, "STATUS_TYPE_"))
	if m.Description != "" {
		fmt.Fprintf(w, "Description:  %s\n", m.Description)
	}
	if len(m.Repositories) > 0 {
		fmt.Fprintf(w, "Repositories: %s\n", strings.Join(m.Repositories, ", "))
	}
	if !m.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "Expires:      %s\n", m.ExpiresAt.Format(time.RFC3339))
	}
	for _, p := range m.RepositoryProgress {
		fmt.Fprintf(w, "Progress (%s): resources %d/%d processed, events %d/%d processed\n",
			p.RepositoryNWO, p.ResourcesProcessed, p.ResourcesAdded, p.EventsProcessed, p.EventsAdded)
	}
	fmt.Fprintln(w)
}

// printMigrationJSON writes a target migration's raw API JSON as one NDJSON
// line, mirroring printResourceJSON in resources.go.
func printMigrationJSON(w io.Writer, m elmapi.TargetMigration) error {
	raw := m.Raw
	if len(raw) == 0 {
		// Fallback for migrations not decoded from an API response (for
		// example constructed in tests); marshal the typed representation.
		var err error
		raw, err = json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshaling migration: %w", err)
		}
	}

	// Compact to a single line so multi-line raw JSON stays valid NDJSON.
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return fmt.Errorf("compacting migration JSON: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

func migrationCountSummary(n int) string {
	switch n {
	case 0:
		return "No migrations found."
	case 1:
		return "Found 1 migration."
	default:
		return fmt.Sprintf("Found %d migrations.", n)
	}
}

// migrationCreateView is the human-facing subset of the create-migration
// response.
type migrationCreateView struct {
	MigrationID string    `json:"migrationId"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func printMigrationCreate(w io.Writer, v migrationCreateView) {
	fmt.Fprintf(w, "Migration %s created.\n", v.MigrationID)
	if !v.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "Expires at: %s\n", v.ExpiresAt.Format(time.RFC3339))
	}
}
