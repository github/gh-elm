package target

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
)

// newCreateReportCmd builds `gh elm target create-report`, which requests a node
// report for a migration. POST /enterprise/migration/:id/reports.
func newCreateReportCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		stateFlag   string
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "create-report",
		Short: "Request a node report for a migration from the target",
		Long: "Request a node report for a migration from the target (GHEC/Proxima) REST API.\n" +
			"The report is generated asynchronously; poll `gh elm target report-status` and\n" +
			"then download it with `gh elm target report-url`. Prints the API's raw JSON response.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stage, err := resolveReportStage(stageFlag)
			if err != nil {
				return err
			}
			state, err := resolveReportState(stateFlag)
			if err != nil {
				return err
			}

			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}

			raw, err := client.CreateReport(cmd.Context(), migrationID, stage, state)
			if err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			return writeRaw(cmd.OutOrStdout(), raw)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to request a report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage the report should cover: backfill or live_updates (required).")
	cmd.Flags().StringVar(&stateFlag, "state", "all", "Node states the report should cover: migrated, unmigrated, or all.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")
	_ = cmd.MarkFlagRequired("stage")

	return cmd
}

// newReportStatusCmd builds `gh elm target report-status`, which queries the
// status of a node report. GET /enterprise/migration/:id/reports/status.
func newReportStatusCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "report-status",
		Short: "Query the status of a migration's node report",
		Long:  "Query the status of a migration's node report from the target (GHEC/Proxima) REST API. Prints the API's raw JSON response.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stage, err := resolveReportStage(stageFlag)
			if err != nil {
				return err
			}

			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}

			raw, err := client.GetReportStatus(cmd.Context(), migrationID, stage)
			if err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			return writeRaw(cmd.OutOrStdout(), raw)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to query the report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage of the report: backfill or live_updates (required).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")
	_ = cmd.MarkFlagRequired("stage")

	return cmd
}

// newReportURLCmd builds `gh elm target report-url`, which returns a short-lived
// signed download URL for a finished report. GET /enterprise/migration/:id/reports/url.
func newReportURLCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "report-url",
		Short: "Get a signed download URL for a finished report",
		Long: "Get a short-lived, read-only signed URL to download a finished node report\n" +
			"archive directly from blob storage. The report must be finished; check with\n" +
			"`gh elm target report-status` first. Prints the API's raw JSON response.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stage, err := resolveReportStage(stageFlag)
			if err != nil {
				return err
			}

			client, targetURLResolved, err := targetClient(targetURL, targetToken)
			if err != nil {
				return err
			}

			raw, err := client.GetReportURL(cmd.Context(), migrationID, stage)
			if err != nil {
				return annotateAuthError(err, targetURLResolved)
			}
			return writeRaw(cmd.OutOrStdout(), raw)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to download the report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage of the report: backfill or live_updates (required).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")
	_ = cmd.MarkFlagRequired("stage")

	return cmd
}

// targetClient resolves the target endpoint (flags override env override stored
// config) and returns a ready client plus the resolved base URL for error
// messages.
func targetClient(targetURL, targetToken string) (*elmapi.Client, string, error) {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return nil, "", err
	}
	ep, err := resolver.Target(targetURL, targetToken)
	if err != nil {
		return nil, "", err
	}
	if ep.URL == "" {
		return nil, "", fmt.Errorf("no target URL configured; run `gh elm configure`, set %s, or pass --target-url", config.EnvTargetURL)
	}
	if ep.Token == "" {
		return nil, "", fmt.Errorf("no target token configured; run `gh elm configure`, set %s, or pass --target-token", config.EnvTargetToken)
	}
	return elmapi.NewClient(ep.URL, ep.Token), ep.URL, nil
}

// resolveReportStage maps the --stage flag to its wire value. The stage is
// required, so an empty flag is an error.
func resolveReportStage(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "backfill":
		return elmapi.ReportStageBackfill, nil
	case "live_updates", "live-updates":
		return elmapi.ReportStageLiveUpdates, nil
	default:
		return "", fmt.Errorf("invalid --stage %q: must be backfill or live_updates", s)
	}
}

// resolveReportState maps the --state flag to its wire value, defaulting to all.
func resolveReportState(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all":
		return elmapi.ReportStateAll, nil
	case "migrated":
		return elmapi.ReportStateMigrated, nil
	case "unmigrated":
		return elmapi.ReportStateUnmigrated, nil
	default:
		return "", fmt.Errorf("invalid --state %q: must be migrated, unmigrated, or all", s)
	}
}

// writeRaw writes the API's raw JSON response followed by a newline. The report
// endpoints return small, structured responses that callers typically pipe to
// jq, so we echo the API JSON verbatim rather than reformat it.
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
