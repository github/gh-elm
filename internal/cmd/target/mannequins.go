package target

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/ghapi"
)

// newMannequinCmd builds the `gh elm target mannequin` command group — `list`
// (fetch a target org's mannequins as CSV) and `claim` (reclaim one or more
// mannequins), ported from gh-gei's generate-mannequin-csv and
// reclaim-mannequin. Both operate on the target (GitHub with Data Residency) organization via
// its GitHub GraphQL/REST API.
func newMannequinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mannequin",
		Short: "List and claim mannequins on the target org",
		Long: "Work with mannequins on a target (GitHub with Data Residency) organization: `list` writes\n" +
			"them as CSV, and `claim` reclaims them (mapping each mannequin to a target user).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newMannequinListCmd())
	cmd.AddCommand(newMannequinClaimCmd())

	return cmd
}

// newMannequinListCmd builds `gh elm target mannequin list`, which lists a
// target org's mannequins and writes them as CSV (to stdout, or a file with
// --output).
func newMannequinListCmd() *cobra.Command {
	var (
		githubOrg        string
		output           string
		includeReclaimed bool
		targetURL        string
		targetToken      string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a target org's mannequins as CSV",
		Long: "List mannequins for a target (GitHub with Data Residency) organization and write them\n" +
			"as CSV. By default the CSV is written to stdout; pass --output to write a file.\n" +
			"The CSV can then be edited and fed to `gh elm target mannequin claim --csv`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, targetURLResolved, err := mannequinClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			log := mannequinLogger{w: cmd.ErrOrStderr()}

			orgID, err := client.OrganizationID(cmd.Context(), githubOrg)
			if err != nil {
				return annotateMannequinAuthError(err, targetURLResolved)
			}

			mannequins, err := client.Mannequins(cmd.Context(), orgID)
			if err != nil {
				return annotateMannequinAuthError(err, targetURLResolved)
			}

			reclaimed := 0
			for _, m := range mannequins {
				if m.MappedUser != nil {
					reclaimed++
				}
			}
			log.Infof("Found %d mannequin(s); %d already reclaimed.", len(mannequins), reclaimed)

			records := ghapi.ToMannequinRecords(mannequins, includeReclaimed)

			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("creating %s: %w", output, err)
				}
				defer func() { _ = f.Close() }()
				if err := ghapi.WriteMannequinCSV(f, records); err != nil {
					return fmt.Errorf("writing %s: %w", output, err)
				}
				log.Infof("Wrote CSV to %s.", output)
				return nil
			}

			return ghapi.WriteMannequinCSV(cmd.OutOrStdout(), records)
		},
	}

	cmd.Flags().StringVar(&githubOrg, "github-org", "", "Target organization login to list mannequins for (required).")
	cmd.Flags().StringVar(&output, "output", "", "Write the CSV to this file instead of stdout.")
	cmd.Flags().BoolVar(&includeReclaimed, "include-reclaimed", false, "Include mannequins that have already been reclaimed.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("github-org")

	return cmd
}

// newMannequinClaimCmd builds `gh elm target mannequin claim`, which reclaims a
// single mannequin or a batch from a CSV against the target org.
func newMannequinClaimCmd() *cobra.Command {
	var (
		githubOrg      string
		csvPath        string
		mannequinUser  string
		mannequinID    string
		targetUser     string
		force          bool
		skipInvitation bool
		noPrompt       bool
		targetURL      string
		targetToken    string
	)

	cmd := &cobra.Command{
		Use:   "claim",
		Short: "Claim (reclaim) one or more mannequins on the target org",
		Long: "Claim mannequins on a target (GitHub with Data Residency) organization, mapping each\n" +
			"mannequin to a target user. Claim a single mannequin with --mannequin-user\n" +
			"and --target-user, or claim many at once with --csv (see `gh elm target\n" +
			"mannequin list`). Use --skip-invitation to reattribute immediately without\n" +
			"the invitation email flow (EMU organizations only).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if csvPath == "" && (mannequinUser == "" || targetUser == "") {
				return errors.New("either --csv or both --mannequin-user and --target-user must be specified")
			}

			client, targetURLResolved, err := mannequinClient(targetURL, targetToken)
			if err != nil {
				return err
			}
			log := mannequinLogger{w: cmd.ErrOrStderr()}
			svc := ghapi.NewReclaimService(client, log)

			if skipInvitation {
				if err := ensureSkipInvitationAllowed(cmd, client, githubOrg, noPrompt); err != nil {
					return annotateMannequinAuthError(err, targetURLResolved)
				}
			}

			if csvPath != "" {
				log.Infof("Claiming mannequins from CSV...")
				f, err := os.Open(csvPath)
				if err != nil {
					return fmt.Errorf("opening %s: %w", csvPath, err)
				}
				defer func() { _ = f.Close() }()
				records, err := ghapi.ReadMannequinCSV(f)
				if err != nil {
					return err
				}
				if err := svc.ReclaimMannequins(cmd.Context(), records, githubOrg, force, skipInvitation); err != nil {
					return annotateMannequinAuthError(err, targetURLResolved)
				}
				return nil
			}

			log.Infof("Claiming mannequin...")
			if err := svc.ReclaimMannequin(cmd.Context(), mannequinUser, mannequinID, targetUser, githubOrg, force, skipInvitation); err != nil {
				return annotateMannequinAuthError(err, targetURLResolved)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&githubOrg, "github-org", "", "Target organization login (required).")
	cmd.Flags().StringVar(&csvPath, "csv", "", "Path to a mannequin CSV (from 'gh elm target mannequin list').")
	cmd.Flags().StringVar(&mannequinUser, "mannequin-user", "", "Login of the mannequin to claim (single mode).")
	cmd.Flags().StringVar(&mannequinID, "mannequin-id", "", "Optional mannequin ID to disambiguate a login (single mode).")
	cmd.Flags().StringVar(&targetUser, "target-user", "", "Login of the target user to claim to (single mode).")
	cmd.Flags().BoolVar(&force, "force", false, "Claim even if the mannequin is already mapped to a user.")
	cmd.Flags().BoolVar(&skipInvitation, "skip-invitation", false, "Reattribute immediately without the invitation email (EMU orgs only).")
	cmd.Flags().BoolVar(&noPrompt, "no-prompt", false, "Do not prompt for confirmation when using --skip-invitation.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.MarkFlagRequired("github-org")

	return cmd
}

// ensureSkipInvitationAllowed verifies the authenticated user is an admin of the
// target org (a precondition for --skip-invitation) and, unless noPrompt is set,
// asks for confirmation since the operation is immediate and irreversible.
func ensureSkipInvitationAllowed(cmd *cobra.Command, client *ghapi.Client, githubOrg string, noPrompt bool) error {
	login, err := client.LoginName(cmd.Context())
	if err != nil {
		return err
	}
	role, err := client.OrgMembership(cmd.Context(), githubOrg, login)
	if err != nil {
		return err
	}
	if role != "admin" {
		return fmt.Errorf("user %s is not an org admin and is not eligible to claim mannequins with --skip-invitation", login)
	}

	if noPrompt {
		return nil
	}
	if !confirm(cmd.InOrStdin(), cmd.ErrOrStderr(),
		"Claiming mannequins with --skip-invitation is immediate and irreversible. Continue? [y/N]") {
		return errors.New("aborted")
	}
	return nil
}

// confirm reads a single line and reports whether it is an affirmative yes.
func confirm(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s ", prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// mannequinClient resolves the target endpoint (flag > env > stored config) and
// returns a ready GitHub API client plus the resolved base URL for error
// messages. It mirrors targetClient but returns a *ghapi.Client for the
// mannequin commands' GraphQL/REST calls.
func mannequinClient(targetURL, targetToken string) (*ghapi.Client, string, error) {
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
	return ghapi.NewClient(ep.URL, ep.Token), ep.URL, nil
}

// annotateMannequinAuthError turns a 401/403 from the target GitHub API into an
// actionable message, matching the other target commands.
func annotateMannequinAuthError(err error, targetURL string) error {
	var httpErr *ghapi.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		//nolint:staticcheck // ST1005: intentional multi-line, user-facing CLI error message
		return fmt.Errorf("authentication failed (HTTP %d) for target %s: %s\n"+
			"Check the target token with `gh elm configure --show`. Note the %s and %s environment variables override stored config.",
			httpErr.StatusCode, targetURL, httpErr.Message, config.EnvTargetURL, config.EnvTargetToken)
	}
	return err
}

// mannequinLogger implements ghapi.Logger, writing progress and warnings to a
// writer (stderr) so command stdout stays clean for machine-readable output.
type mannequinLogger struct{ w io.Writer }

func (l mannequinLogger) Infof(format string, args ...any) {
	fmt.Fprintf(l.w, format+"\n", args...)
}

func (l mannequinLogger) Warnf(format string, args ...any) {
	fmt.Fprintf(l.w, "warning: "+format+"\n", args...)
}
