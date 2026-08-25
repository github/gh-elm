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
	"github.com/github/gh-elm/internal/render"
)

// newMannequinCmd builds the `gh elm target mannequin` command group — `list`
// (fetch a target org's mannequins as CSV) and `reclaim` (reclaim one or more
// mannequins), ported from gh-gei's generate-mannequin-csv and
// reclaim-mannequin. Both operate on the target (GitHub with Data Residency) organization via
// its GitHub GraphQL/REST API.
func newMannequinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mannequin",
		Short: "List and reclaim mannequins on the target org",
		Long: "Work with mannequins on a target (GitHub with Data Residency) organization: `list` writes\n" +
			"them as CSV, and `reclaim` maps each mannequin to a target user.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newMannequinListCmd())
	cmd.AddCommand(newMannequinReclaimCmd())
	cmd.AddCommand(newMannequinClaimCmd())

	return cmd
}

// newMannequinListCmd builds `gh elm target mannequin list`, which lists a
// target org's mannequins and writes them as CSV (to stdout, or a file with
// --output).
func newMannequinListCmd() *cobra.Command {
	var (
		orgFlag          string
		githubOrgFlag    string
		output           string
		includeReclaimed bool
		targetURL        string
		targetToken      string
	)

	cmd := &cobra.Command{
		Use:   "list [ORGANIZATION]",
		Short: "List a target org's mannequins as CSV",
		Long: "List mannequins for a target (GitHub with Data Residency) organization and write them\n" +
			"as CSV. By default the CSV is written to stdout; pass --output to write a file.\n" +
			"The CSV can then be edited and fed to `gh elm target mannequin reclaim --csv`.",
		Example: "  gh elm target mannequin list octo-org\n" +
			"  gh elm target mannequin list octo-org --include-reclaimed --output mannequins.csv",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			orgFromFlag, orgFlagName, orgFlagChanged, err := resolveAliasedFlag(
				"org", orgFlag, cmd.Flags().Changed("org"),
				"github-org", githubOrgFlag, cmd.Flags().Changed("github-org"),
			)
			if err != nil {
				return err
			}
			var positionalOrg string
			if len(args) == 1 {
				positionalOrg = args[0]
			}
			githubOrg, err := resolveStringOperand("ORGANIZATION", positionalOrg, orgFlagName, orgFromFlag, orgFlagChanged)
			if err != nil {
				return err
			}

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
				log.Successf("Wrote CSV to %s.", output)
				return nil
			}

			return ghapi.WriteMannequinCSV(cmd.OutOrStdout(), records)
		},
	}

	cmd.Flags().StringVar(&orgFlag, "org", "", "Target organization login (alternative to the positional argument).")
	cmd.Flags().StringVar(&githubOrgFlag, "github-org", "", "Target organization login (legacy alias for --org).")
	cmd.Flags().StringVar(&output, "output", "", "Write the CSV to this file instead of stdout.")
	cmd.Flags().BoolVar(&includeReclaimed, "include-reclaimed", false, "Include mannequins that have already been reclaimed.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.Flags().MarkHidden("github-org")

	return cmd
}

// newMannequinReclaimCmd builds `gh elm target mannequin reclaim`, which reclaims a
// single mannequin or a batch from a CSV against the target org.
func newMannequinReclaimCmd() *cobra.Command {
	var (
		orgFlag        string
		githubOrgFlag  string
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
		Use:   "reclaim [ORGANIZATION] [MANNEQUIN] [TARGET-USER]",
		Short: "Reclaim one or more mannequins on the target org",
		Long: "Reclaim mannequins on a target (GitHub with Data Residency) organization, mapping each\n" +
			"mannequin to a target user. Pass the organization, mannequin, and target user\n" +
			"positionally, or reclaim many at once with --csv (see `gh elm target\n" +
			"mannequin list`). Use --skip-invitation to reattribute immediately without\n" +
			"the invitation email flow (EMU organizations only).",
		Example: "  gh elm target mannequin reclaim octo-org mannequin-login target-login\n" +
			"  gh elm target mannequin reclaim octo-org --csv mannequins.csv",
		Args: cobra.MaximumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			orgFromFlag, orgFlagName, orgFlagChanged, err := resolveAliasedFlag(
				"org", orgFlag, cmd.Flags().Changed("org"),
				"github-org", githubOrgFlag, cmd.Flags().Changed("github-org"),
			)
			if err != nil {
				return err
			}

			var positionalOrg string
			argIndex := 0
			if !orgFlagChanged && argIndex < len(args) {
				positionalOrg = args[argIndex]
				argIndex++
			}
			githubOrg, err := resolveStringOperand("ORGANIZATION", positionalOrg, orgFlagName, orgFromFlag, orgFlagChanged)
			if err != nil {
				return err
			}

			if csvPath != "" {
				if argIndex < len(args) || cmd.Flags().Changed("mannequin-user") || cmd.Flags().Changed("target-user") {
					return errors.New("--csv cannot be combined with MANNEQUIN, TARGET-USER, --mannequin-user, or --target-user")
				}
			} else {
				var positionalMannequin, positionalTarget string
				if !cmd.Flags().Changed("mannequin-user") && argIndex < len(args) {
					positionalMannequin = args[argIndex]
					argIndex++
				}
				if !cmd.Flags().Changed("target-user") && argIndex < len(args) {
					positionalTarget = args[argIndex]
					argIndex++
				}
				if argIndex < len(args) {
					return errors.New("positional organization or user duplicates a value already supplied by flag")
				}
				mannequinUser, err = resolveStringOperand(
					"MANNEQUIN", positionalMannequin, "mannequin-user", mannequinUser, cmd.Flags().Changed("mannequin-user"),
				)
				if err != nil {
					return err
				}
				targetUser, err = resolveStringOperand(
					"TARGET-USER", positionalTarget, "target-user", targetUser, cmd.Flags().Changed("target-user"),
				)
				if err != nil {
					return err
				}
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
				log.Infof("Reclaiming mannequins from CSV...")
				f, err := os.Open(csvPath)
				if err != nil {
					return fmt.Errorf("opening %s: %w", csvPath, err)
				}
				defer func() { _ = f.Close() }()
				records, err := ghapi.ReadMannequinCSV(f)
				if err != nil {
					return err
				}
				if err := confirmBotReclaims(cmd, log, records, noPrompt); err != nil {
					return err
				}
				if err := svc.ReclaimMannequins(cmd.Context(), records, githubOrg, force, skipInvitation); err != nil {
					return annotateMannequinAuthError(err, targetURLResolved)
				}
				return nil
			}
			log.Infof("Reclaiming mannequin...")
			if err := confirmBotReclaims(cmd, log, []ghapi.MannequinRecord{{MannequinUser: mannequinUser, TargetUser: targetUser}}, noPrompt); err != nil {
				return err
			}
			if err := svc.ReclaimMannequin(cmd.Context(), mannequinUser, mannequinID, targetUser, githubOrg, force, skipInvitation); err != nil {
				return annotateMannequinAuthError(err, targetURLResolved)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&orgFlag, "org", "", "Target organization login (alternative to the positional argument).")
	cmd.Flags().StringVar(&githubOrgFlag, "github-org", "", "Target organization login (legacy alias for --org).")
	cmd.Flags().StringVar(&csvPath, "csv", "", "Path to a mannequin CSV (from 'gh elm target mannequin list').")
	cmd.Flags().StringVar(&mannequinUser, "mannequin-user", "", "Mannequin login (alternative to the positional argument).")
	cmd.Flags().StringVar(&mannequinID, "mannequin-id", "", "Optional mannequin ID to disambiguate a login (single mode).")
	cmd.Flags().StringVar(&targetUser, "target-user", "", "Target user login (alternative to the positional argument).")
	cmd.Flags().BoolVar(&force, "force", false, "Reclaim even if the mannequin is already mapped to a user.")
	cmd.Flags().BoolVar(&skipInvitation, "skip-invitation", false, "Reattribute immediately without the invitation email (EMU orgs only).")
	cmd.Flags().BoolVar(&noPrompt, "no-prompt", false, "Do not prompt for confirmation (--skip-invitation or bot reclaims).")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	_ = cmd.Flags().MarkHidden("github-org")

	return cmd
}

func newMannequinClaimCmd() *cobra.Command {
	cmd := newMannequinReclaimCmd()
	cmd.Use = "claim"
	cmd.Hidden = true
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

// confirmBotReclaims warns about likely mis-targets and, unless noPrompt is set,
// asks for confirmation before an irreversible bot reattribution. Reattributing
// to a bot auto-accepts and cannot be undone. The source mannequin's login is
// our only hint that it represents a bot; a non-"[bot]" source is very likely a
// mis-target (a human's content going to a bot), but the convention is
// GitHub-specific, so we warn and let the admin proceed rather than blocking.
func confirmBotReclaims(cmd *cobra.Command, log mannequinLogger, records []ghapi.MannequinRecord, noPrompt bool) error {
	var botCount, humanCount int
	warned := make(map[string]bool)
	var firstBot ghapi.MannequinRecord
	for _, r := range records {
		if !ghapi.IsBotLogin(r.TargetUser) {
			humanCount++
			continue
		}
		if botCount == 0 {
			firstBot = r
		}
		botCount++
		if !ghapi.IsBotLogin(r.MannequinUser) && !warned[r.MannequinUser] {
			warned[r.MannequinUser] = true
			log.Warnf("%q does not look like a bot mannequin (its login does not end in %q). Are you sure you want to do this?", r.MannequinUser, "[bot]")
		}
	}

	if botCount == 0 || noPrompt {
		return nil
	}

	var summary string
	if len(records) > 1 {
		summary = fmt.Sprintf("You are about to reattribute %d mannequin(s) to GitHub App / bot account(s)", botCount)
		if humanCount > 0 {
			summary += fmt.Sprintf(" and %d mannequin(s) to user(s)", humanCount)
		}
		summary += "."
	} else {
		summary = fmt.Sprintf("You are about to reattribute every mannequin identity matching %q to the GitHub App / bot account %q.", firstBot.MannequinUser, firstBot.TargetUser)
	}

	if !confirm(cmd.InOrStdin(), cmd.ErrOrStderr(),
		summary+" Reattributing content to a bot is immediate and cannot be undone. Continue? [y/N]") {
		return errors.New("aborted")
	}
	return nil
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
		return nil, "", fmt.Errorf("no target URL configured; run `gh elm config`, set %s, or pass --target-url", config.EnvTargetURL)
	}
	if ep.Token == "" {
		return nil, "", fmt.Errorf("no target token configured; run `gh elm config`, set %s, or pass --target-token", config.EnvTargetToken)
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
			"Check the target token with `gh elm config show`. Note the %s and %s environment variables override stored config.",
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

func (l mannequinLogger) Successf(format string, args ...any) {
	fmt.Fprintln(l.w, render.Success(fmt.Sprintf(format, args...)))
}

func (l mannequinLogger) Warnf(format string, args ...any) {
	fmt.Fprintln(l.w, render.Warning(fmt.Sprintf(format, args...)))
}
