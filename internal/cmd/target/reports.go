package target

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/render"
)

// newReportCmd builds the `gh elm target report` command group — `create`,
// `status`, and `url` — for a migration's node reports.
func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Create, check, and download a migration's node reports",
		Long: "Work with a migration's node reports on the target (GitHub with Data Residency) side:\n" +
			"`create` requests one, `status` polls it, and `url` returns a signed download URL.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newReportCreateCmd())
	cmd.AddCommand(newReportStatusCmd())
	cmd.AddCommand(newReportURLCmd())
	return cmd
}

// newReportCreateCmd builds `gh elm target report create`, which requests a node
// report for a migration. POST /enterprise/migration/:id/reports.
func newReportCreateCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		stateFlag   string
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Request a node report for a migration",
		Long: "Request a node report for a migration from the target (GitHub with Data Residency) REST API.\n" +
			"The report is generated asynchronously; poll `gh elm target report status` and\n" +
			"then download it with `gh elm target report url`.",
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
			return renderReport(cmd.OutOrStdout(), raw, asJSON, printReportCreate)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to request a report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage the report should cover: backfill or live_updates (required).")
	cmd.Flags().StringVar(&stateFlag, "state", "all", "Node states the report should cover: migrated, unmigrated, or all.")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the API's raw JSON response instead of human-readable text.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")
	_ = cmd.MarkFlagRequired("stage")

	return cmd
}

// newReportStatusCmd builds `gh elm target report status`, which queries the
// status of a node report. GET /enterprise/migration/:id/reports/status.
func newReportStatusCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Query the status of a migration's node report",
		Long:  "Query a migration's node report status from the target (GitHub with Data Residency) REST API.",
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
			return renderReport(cmd.OutOrStdout(), raw, asJSON, printReportStatus)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to query the report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage of the report: backfill or live_updates (required).")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the API's raw JSON response instead of human-readable text.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("migration-id")
	_ = cmd.MarkFlagRequired("stage")

	return cmd
}

// newReportURLCmd builds `gh elm target report url`, which returns a short-lived
// signed download URL for a finished report. GET /enterprise/migration/:id/reports/url.
func newReportURLCmd() *cobra.Command {
	var (
		migrationID int64
		stageFlag   string
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "url",
		Short: "Get a signed download URL for a finished report",
		Long: "Get a short-lived, read-only signed URL to download a finished node report\n" +
			"archive directly from blob storage. The report must be finished; check with\n" +
			"`gh elm target report status` first.",
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
			return renderReport(cmd.OutOrStdout(), raw, asJSON, printReportURL)
		},
	}

	cmd.Flags().Int64Var(&migrationID, "migration-id", 0, "Migration ID to download the report for (required).")
	cmd.Flags().StringVar(&stageFlag, "stage", "", "Migration stage of the report: backfill or live_updates (required).")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the API's raw JSON response instead of human-readable text.")
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

// renderReport writes either the API's raw JSON (asJSON) or a human-readable
// rendering parsed from it. The raw path echoes the response verbatim so unknown
// fields survive and no zero values are fabricated; the human path only reads
// the fields it displays.
func renderReport[T any](out io.Writer, raw json.RawMessage, asJSON bool, renderView func(io.Writer, T)) error {
	if asJSON {
		return writeRaw(out, raw)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("parsing report response: %w", err)
	}
	// Render into a buffer first, then write to out once so a failed output
	// stream (e.g. a closed pipe) surfaces as an error instead of a silently
	// truncated success. Writes to a bytes.Buffer can't fail, so the render
	// callbacks don't need to return errors.
	var buf bytes.Buffer
	renderView(&buf, v)
	return render.Write(out, buf.String())
}

// reportCreateView is the human-facing subset of the create-report response.
type reportCreateView struct {
	RequestedAt       time.Time `json:"requestedAt"`
	AlreadyInProgress bool      `json:"alreadyInProgress"`
}

func printReportCreate(w io.Writer, v reportCreateView) {
	if v.AlreadyInProgress {
		fmt.Fprintln(w, render.Success("A report for this stage was already in progress; reusing it."))
	} else {
		fmt.Fprintln(w, render.Success("Report requested."))
	}
	if !v.RequestedAt.IsZero() {
		fmt.Fprintf(w, "Requested at: %s\n", v.RequestedAt.Format(time.RFC3339))
	}
}

// reportStatusView is the human-facing subset of the report-status response.
type reportStatusView struct {
	Status         string    `json:"status"`
	TotalSizeBytes string    `json:"totalSizeBytes"`
	Stage          string    `json:"stage"`
	State          string    `json:"state"`
	RequestedAt    time.Time `json:"requestedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	Format         string    `json:"format"`
	Files          []struct {
		Name      string `json:"name"`
		SizeBytes string `json:"sizeBytes"`
	} `json:"files"`
}

func printReportStatus(w io.Writer, v reportStatusView) {
	fmt.Fprintf(w, "Status: %s\n", friendlyEnum(v.Status, "REPORT_STATUS_"))
	if v.Stage != "" {
		fmt.Fprintf(w, "Stage:  %s\n", friendlyEnum(v.Stage, "REPORT_STAGE_"))
	}
	if v.State != "" {
		fmt.Fprintf(w, "State:  %s\n", friendlyEnum(v.State, "REPORT_STATE_"))
	}
	if !v.RequestedAt.IsZero() {
		fmt.Fprintf(w, "Requested: %s\n", v.RequestedAt.Format(time.RFC3339))
	}
	if !v.FinishedAt.IsZero() {
		fmt.Fprintf(w, "Finished:  %s\n", v.FinishedAt.Format(time.RFC3339))
	}
	if v.Format != "" {
		fmt.Fprintf(w, "Format: %s\n", v.Format)
	}
	if v.TotalSizeBytes != "" {
		fmt.Fprintf(w, "Total size (bytes): %s\n", v.TotalSizeBytes)
	}
	for _, f := range v.Files {
		fmt.Fprintf(w, "  - %s (%s bytes)\n", f.Name, f.SizeBytes)
	}
}

// reportURLView is the human-facing subset of the report-url response.
type reportURLView struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// printReportURL prints the signed URL on its own first line, intentionally
// unlabeled (unlike the other human-readable fields) so callers can grab it with
// `head -1` without switching to --json. The expiry follows on a labeled line.
func printReportURL(w io.Writer, v reportURLView) {
	fmt.Fprintln(w, v.URL)
	if !v.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "Expires at: %s\n", v.ExpiresAt.Format(time.RFC3339))
	}
}
