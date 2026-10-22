package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/ghapi"
	"github.com/github/gh-elm/internal/render"
	"github.com/github/gh-elm/internal/theme"
)

// newConfigCmd builds the `gh elm config` command: an interactive setup
// for the source (GHES) and target (GitHub with Data Residency) API URLs and tokens.
func newConfigCmd() *cobra.Command {
	var (
		showFlag  bool
		resetFlag bool
	)

	cmd := &cobra.Command{
		Use:         "config",
		Short:       "Interactively set up credentials for gh elm",
		Annotations: map[string]string{directActionAnnotation: "true"},
		Long: "Configure the source (GHES) and target (GitHub with Data Residency) API URLs and tokens that\n" +
			"gh elm uses.\n\n" +
			"URLs are saved to gh-elm's config file. Tokens are stored in your OS keyring\n" +
			"(macOS Keychain, Linux Secret Service, Windows Credential Manager) when one is\n" +
			"available, otherwise in a 0600 credentials file; set GH_ELM_CREDENTIAL_STORE to\n" +
			"\"file\" or \"keyring\" to force a backend.\n\n" +
			"Environment variables (GH_SOURCE_HOST/GH_SOURCE_TOKEN, GH_TARGET_HOST/\n" +
			"GH_TARGET_TOKEN) and command flags override the stored values, so scripts\n" +
			"and CI can skip this command entirely.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Reset must clear every backend, so it doesn't depend on the single
			// store NewStore would select for this invocation.
			if resetFlag {
				return runConfigureReset(cmd)
			}
			store, err := creds.NewStore()
			if err != nil {
				return err
			}
			if showFlag {
				return runConfigureShow(cmd, store)
			}
			return runConfigureInteractive(cmd, store)
		},
	}

	cmd.Flags().BoolVar(&showFlag, "show", false, "Print the current configuration (tokens redacted)")
	cmd.Flags().BoolVar(&resetFlag, "reset", false, "Remove stored configuration and credentials")
	cmd.MarkFlagsMutuallyExclusive("show", "reset")
	_ = cmd.Flags().MarkHidden("show")
	_ = cmd.Flags().MarkHidden("reset")
	cmd.AddCommand(
		newConfigShowCmd(),
		newConfigResetCmd(),
		newConfigConfirmAccessCmd(),
		newSetMigratorPATInteractiveCmd(),
		newSetMigratorPATCmd("set-source-pat", "SOURCE_PAT"),
		newSetMigratorPATCmd("set-target-pat", "TARGET_PAT"),
	)

	return cmd
}

var (
	configCredentialRequiredScopes = []string{"admin:enterprise", "admin:org"}
	sourcePATRequiredScopes        = []string{"repo", "admin:org", "admin:repo_hook", "admin:org_hook"}
	targetPATRequiredScopes        = []string{"repo", "workflow", "admin:org", "admin:repo_hook", "admin:enterprise"}
)

type configCredentialKind int

const (
	sourceConfigCredential configCredentialKind = iota
	targetConfigCredential
)

func validateConfigCredential(
	ctx context.Context,
	kind configCredentialKind,
	rawURL string,
	token string,
) tokenValidationResult {
	if strings.TrimSpace(rawURL) == "" {
		return tokenValidationResult{status: tokenInvalid, cause: errors.New("API URL is not configured")}
	}
	if token == "" {
		return tokenValidationResult{status: tokenInvalid, cause: errors.New("token is not configured")}
	}

	apiURL := endpoints.NormalizeSourceAPIURL(rawURL)
	if kind == targetConfigCredential {
		apiURL = endpoints.NormalizeTargetAPIURL(rawURL)
	}
	return validateTokenScopes(ctx, apiURL, token, configCredentialRequiredScopes)
}

type validationDecision int

const (
	validationContinue validationDecision = iota
	validationRetry
	validationAbort
)

type tokenValidationStatus int

const (
	tokenValid tokenValidationStatus = iota
	tokenInvalid
	tokenValidationWarning
)

type tokenValidationResult struct {
	status        tokenValidationStatus
	missingScopes []string
	cause         error
}

func validateTokenScopes(ctx context.Context, apiURL, token string, requiredScopes []string) tokenValidationResult {
	if apiURL == "" {
		return tokenValidationResult{status: tokenValidationWarning, cause: errors.New("API URL is not configured")}
	}

	grantedScopes, err := ghapi.NewClient(apiURL, token).TokenScopes(ctx)
	if err != nil {
		var httpErr *ghapi.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized {
			return tokenValidationResult{status: tokenInvalid, cause: errors.New("GitHub rejected the token")}
		}
		return tokenValidationResult{status: tokenValidationWarning, cause: err}
	}

	granted := make(map[string]struct{}, len(grantedScopes))
	for _, scope := range grantedScopes {
		granted[scope] = struct{}{}
	}
	var missing []string
	for _, scope := range requiredScopes {
		if !tokenGrantsScope(granted, scope) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return tokenValidationResult{status: tokenInvalid, missingScopes: missing}
	}
	return tokenValidationResult{status: tokenValid}
}

func tokenGrantsScope(granted map[string]struct{}, required string) bool {
	if _, ok := granted[required]; ok {
		return true
	}
	_, hasRepo := granted["repo"]
	return required == "admin:repo_hook" && hasRepo
}

type tokenValidationError struct {
	tokenName     string
	missingScopes []string
	cause         error
}

func (e *tokenValidationError) Error() string {
	if len(e.missingScopes) > 0 {
		return fmt.Sprintf("%s is missing required scopes: %s", e.tokenName, strings.Join(e.missingScopes, ", "))
	}
	return fmt.Sprintf("%s is invalid: %v", e.tokenName, e.cause)
}

func handleConfigCredentialValidation(
	cmd *cobra.Command,
	tokenName string,
	result tokenValidationResult,
	choose func() (validationDecision, error),
) (validationDecision, error) {
	styles := theme.New()
	switch result.status {
	case tokenValid:
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s %s has all required scopes.\n", styles.Success.Render("✓"), tokenName)
		return validationContinue, err
	case tokenInvalid:
		validationErr := &tokenValidationError{tokenName: tokenName, missingScopes: result.missingScopes, cause: result.cause}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", styles.Failure.Render("✗"), validationErr.Error()); err != nil {
			return validationAbort, err
		}
	case tokenValidationWarning:
		if _, err := fmt.Fprintf(
			cmd.ErrOrStderr(),
			"%s Could not validate %s scopes: %v.\n",
			styles.Warning.Render("⚠"),
			tokenName,
			result.cause,
		); err != nil {
			return validationAbort, err
		}
	default:
		return validationAbort, errors.New("unknown token validation status")
	}
	return choose()
}

func runConfigCredentialPhase(
	cmd *cobra.Command,
	tokenName string,
	kind configCredentialKind,
	rawURL *string,
	token *string,
	collect func() error,
	choose func() (validationDecision, error),
) error {
	for {
		if err := collect(); err != nil {
			return err
		}
		result := validateConfigCredential(cmd.Context(), kind, *rawURL, *token)
		decision, err := handleConfigCredentialValidation(cmd, tokenName, result, choose)
		if err != nil {
			return err
		}
		switch decision {
		case validationContinue:
			return nil
		case validationRetry:
			*token = ""
			continue
		case validationAbort:
			return errors.New("configuration cancelled")
		default:
			return errors.New("unknown validation decision")
		}
	}
}

func reportTokenValidation(cmd *cobra.Command, tokenName string, result tokenValidationResult, interactive bool) error {
	styles := theme.New()
	switch result.status {
	case tokenValid:
		if interactive {
			_, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s %s has all required scopes.\n", styles.Success.Render("✓"), tokenName)
			return err
		}
		return nil
	case tokenInvalid:
		return &tokenValidationError{tokenName: tokenName, missingScopes: result.missingScopes, cause: result.cause}
	case tokenValidationWarning:
		_, err := fmt.Fprintf(
			cmd.ErrOrStderr(),
			"%s Could not validate %s scopes: %v.\n",
			styles.Warning.Render("⚠"),
			tokenName,
			result.cause,
		)
		return err
	default:
		return errors.New("unknown token validation status")
	}
}

func newSetMigratorPATCmd(commandName, secretName string) *cobra.Command {
	var org, sourceURL, sourceToken, targetURL, body string

	cmd := &cobra.Command{
		Use:   commandName + " ORG",
		Short: "Set the " + secretName + " used by the migrator",
		Long: "Set the " + secretName + " secret used by the migrator for an organization on the configured\n" +
			"source GHES appliance. Provide the PAT through --body, the " + secretName + " environment\n" +
			"variable, or standard input. When no value is available, the command prompts securely.\n" +
			"The configured source API URL and admin token authenticate this operation.",
		Example: "  " + secretName + "=ghp_example gh elm config " + commandName + " octo-org\n" +
			"  gh elm config " + commandName + " octo-org --body \"$" + secretName + "\"\n" +
			"  gh elm config " + commandName + " --org octo-org < pat.txt",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveOrganization(args, org, cmd.Flags().Changed("org"))
			if err != nil {
				return err
			}

			pat, err := migratorPATInput(cmd, body, secretName)
			if err != nil {
				return err
			}

			resolver, err := endpoints.NewResolver()
			if err != nil {
				return err
			}
			ep, err := resolver.Source(sourceURL, sourceToken)
			if err != nil {
				return err
			}
			if ep.URL == "" {
				return fmt.Errorf("no source URL configured; run `gh elm config`, set %s, or pass --source-url", config.EnvSourceURL)
			}
			if ep.Token == "" {
				return fmt.Errorf("no source admin token configured; run `gh elm config`, set %s, or pass --source-token", config.EnvSourceToken)
			}

			validationURL := ep.URL
			requiredScopes := sourcePATRequiredScopes
			if secretName == "TARGET_PAT" {
				targetEP, targetErr := resolver.Target(targetURL, pat)
				if targetErr != nil {
					return targetErr
				}
				validationURL = targetEP.URL
				requiredScopes = targetPATRequiredScopes
			}
			result := validateTokenScopes(cmd.Context(), validationURL, pat, requiredScopes)
			if err := reportTokenValidation(cmd, secretName, result, outputIsTerminal(cmd.OutOrStdout())); err != nil {
				return err
			}

			if err := elmapi.NewClient(ep.URL, ep.Token).SetMigratorSecret(cmd.Context(), resolvedOrg, secretName, pat); err != nil {
				return err
			}
			return render.Write(cmd.OutOrStdout(), render.Success("Set "+secretName+" migrator secret."))
		},
	}

	cmd.Flags().StringVarP(&body, "body", "b", "", "The PAT value (reads from standard input if not specified).")
	cmd.Flags().StringVarP(&org, "org", "o", "", "Organization that can access the secret.")
	cmd.Flags().StringVar(&sourceURL, "source-url", "", "Override the source (GHES) API base URL.")
	cmd.Flags().StringVar(&sourceToken, "source-token", "", "Override the source (GHES) admin token.")
	if secretName == "TARGET_PAT" {
		cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target GitHub API base URL for PAT validation.")
	}
	return cmd
}

func outputIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	//nolint:gosec // terminal file descriptors fit in int on supported platforms
	return term.IsTerminal(int(file.Fd()))
}

func newSetMigratorPATInteractiveCmd() *cobra.Command {
	var org, sourceURL, sourceToken, targetURL string

	cmd := &cobra.Command{
		Use:   "org-tokens ORG",
		Short: "Interactively set the source and target PATs used by the migrator",
		Long: "Interactively set the source and target PATs used by the migrator for an organization on the configured\n" +
			"source GHES appliance. The configured source API URL and admin token authenticate migration operations.\n\n" +
			"These PATs are stored as secrets on the source GHES appliance and are not stored locally.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedOrg, err := resolveOrganization(args, org, cmd.Flags().Changed("org"))
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("source-url") {
				sourceURL, _ = cmd.Flags().GetString("source-url")
			}
			if cmd.Flags().Changed("source-token") {
				sourceToken, _ = cmd.Flags().GetString("source-token")
			}

			return runSetMigratorPATInteractive(cmd, resolvedOrg, sourceURL, sourceToken, targetURL)
		},
	}

	cmd.Flags().StringVarP(&org, "org", "o", "", "Organization that can access the secret.")
	cmd.Flags().StringVar(&sourceURL, "source-url", "", "Override the source (GHES) API base URL.")
	cmd.Flags().StringVar(&sourceToken, "source-token", "", "Override the source (GHES) admin token.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target GitHub API base URL for PAT validation.")

	return cmd
}

func resolveOrganization(args []string, flagValue string, flagSpecified bool) (string, error) {
	if len(args) > 0 && flagSpecified {
		return "", errors.New("ORG cannot be combined with --org")
	}
	if len(args) == 1 {
		org := strings.TrimSpace(args[0])
		if org == "" {
			return "", errors.New("ORG cannot be empty")
		}
		return org, nil
	}
	if flagSpecified {
		org := strings.TrimSpace(flagValue)
		if org != "" {
			return org, nil
		}
	}
	return "", errors.New("organization required: pass ORG or use --org")
}

func migratorPATInput(cmd *cobra.Command, body, envName string) (string, error) {
	if body != "" {
		return body, nil
	}
	if value := os.Getenv(envName); value != "" {
		return value, nil
	}

	input := cmd.InOrStdin()
	if file, ok := input.(*os.File); ok {
		//nolint:gosec // stdin descriptors fit in int on supported platforms
		fd := int(file.Fd())
		if term.IsTerminal(fd) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Paste %s: ", envName)
			value, err := term.ReadPassword(fd)
			fmt.Fprintln(cmd.ErrOrStderr())
			if err != nil {
				return "", fmt.Errorf("reading PAT: %w", err)
			}
			if len(value) == 0 {
				return "", errors.New("interactive input did not contain a PAT")
			}
			return string(value), nil
		}
	}

	value, err := io.ReadAll(input)
	if err != nil {
		return "", fmt.Errorf("reading PAT from standard input: %w", err)
	}
	value = bytes.TrimRight(value, "\r\n")
	if len(value) == 0 {
		return "", errors.New("standard input did not contain a PAT")
	}
	return string(value), nil
}

func newConfigureAliasCmd() *cobra.Command {
	cmd := newConfigCmd()
	cmd.Use = "configure"
	cmd.Hidden = true
	return cmd
}

func newConfigConfirmAccessCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "confirm-access",
		Short: "Confirm that configured CLI credentials can access their GitHub APIs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			failure := runConfirmAccessChecks(cmd)
			if failure == nil {
				return nil
			}

			interactive := confirmAccessIsInteractive(
				check,
				outputIsTerminal(cmd.OutOrStdout()),
				inputIsTerminal(cmd.InOrStdin()),
			)
			return handleConfirmAccessFailure(
				failure,
				interactive,
				promptRunConfigurationWizard,
				func() error {
					store, err := creds.NewStore()
					if err != nil {
						return err
					}
					return runConfigureInteractive(cmd, store)
				},
				func() error { return runConfirmAccessChecks(cmd) },
			)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Check access without offering interactive reconfiguration")
	return cmd
}

func runConfirmAccessChecks(cmd *cobra.Command) error {
	resolver, err := endpoints.NewResolver()
	if err != nil {
		return err
	}

	source, err := resolver.Source("", "")
	if err != nil {
		return err
	}
	target, err := resolver.Target("", "")
	if err != nil {
		return err
	}

	failed := false
	sourceResult := validateConfigCredential(cmd.Context(), sourceConfigCredential, source.URL, source.Token)
	if err := reportConfirmAccessValidation(cmd, "Source CLI token", sourceResult); err != nil {
		failed = true
	}
	if target.URL != "" {
		targetResult := validateConfigCredential(cmd.Context(), targetConfigCredential, target.URL, target.Token)
		if err := reportConfirmAccessValidation(cmd, "Target CLI token", targetResult); err != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("one or more configured CLI credentials could not be confirmed")
	}
	return nil
}

func reportConfirmAccessValidation(cmd *cobra.Command, tokenName string, result tokenValidationResult) error {
	styles := theme.New()
	switch result.status {
	case tokenValid:
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s has all required scopes.\n", styles.Success.Render("✓"), tokenName)
		return err
	case tokenInvalid:
		validationErr := &tokenValidationError{tokenName: tokenName, missingScopes: result.missingScopes, cause: result.cause}
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", styles.Failure.Render("✗"), validationErr.Error())
		if err != nil {
			return err
		}
		return validationErr
	case tokenValidationWarning:
		_, err := fmt.Fprintf(
			cmd.ErrOrStderr(),
			"%s Could not validate %s scopes: %v.\n",
			styles.Warning.Render("⚠"),
			tokenName,
			result.cause,
		)
		if err != nil {
			return err
		}
		return fmt.Errorf("could not validate %s: %w", tokenName, result.cause)
	default:
		return errors.New("unknown token validation status")
	}
}

func promptRunConfigurationWizard() (bool, error) {
	confirmed := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Would you like to run the configuration wizard?").
				Value(&confirmed),
		),
	).WithTheme(theme.Form())
	if err := runForm(form); err != nil {
		return false, err
	}
	return confirmed, nil
}

func confirmAccessIsInteractive(check, outputTerminal, inputTerminal bool) bool {
	return !check && outputTerminal && inputTerminal
}

func inputIsTerminal(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	//nolint:gosec // terminal file descriptors fit in int on supported platforms
	return term.IsTerminal(int(file.Fd()))
}

func handleConfirmAccessFailure(
	failure error,
	interactive bool,
	confirmWizard func() (bool, error),
	runWizard func() error,
	recheckAccess func() error,
) error {
	if !interactive {
		return failure
	}
	confirmed, err := confirmWizard()
	if err != nil {
		return err
	}
	if !confirmed {
		return failure
	}
	if err := runWizard(); err != nil {
		return err
	}
	return recheckAccess()
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the current configuration with tokens redacted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := creds.NewStore()
			if err != nil {
				return err
			}
			return runConfigureShow(cmd, store)
		},
	}
}

func newConfigResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Remove stored configuration and credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigureReset(cmd)
		},
	}
}

func promptConfigValidationDecision(tokenName string) (validationDecision, error) {
	decision := validationRetry
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[validationDecision]().
				Title(tokenName+" could not be validated. What would you like to do?").
				Options(
					huh.NewOption("Continue anyway", validationContinue),
					huh.NewOption("Try again", validationRetry),
					huh.NewOption("Abort", validationAbort),
				).
				Value(&decision),
		),
	).WithTheme(theme.Form())
	if err := runForm(form); err != nil {
		return validationAbort, err
	}
	return decision, nil
}

func runConfigureInteractive(cmd *cobra.Command, store creds.Store) error {
	//nolint:gosec // os.Stdin.Fd() returns a small, non-negative file descriptor that always fits in an int
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("gh elm config is interactive and needs a terminal; " +
			"in non-interactive environments set GH_SOURCE_HOST/GH_SOURCE_TOKEN (and " +
			"GH_TARGET_HOST/GH_TARGET_TOKEN)")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sourceURL := cfg.SourceURL
	var sourceToken string
	configureTarget := cfg.TargetURL != ""
	targetURL := cfg.TargetURL
	var targetToken string

	if err := runConfigCredentialPhase(
		cmd,
		"Source token",
		sourceConfigCredential,
		&sourceURL,
		&sourceToken,
		func() error {
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewNote().
						Title("Source — GitHub Enterprise Server").
						Description("The GHES appliance you are migrating from."),
					huh.NewInput().
						Title("Source URL").
						Placeholder("https://ghes.example.com").
						Value(&sourceURL).
						Validate(validateURL),
					huh.NewInput().
						Title("Source token").
						Description("A personal access token with the admin:enterprise and admin:org scopes.").
						EchoMode(huh.EchoModePassword).
						Value(&sourceToken).
						Validate(validateRequired("a source token")),
				),
			).WithTheme(theme.Form())
			return runForm(form)
		},
		func() (validationDecision, error) {
			return promptConfigValidationDecision("Source token")
		},
	); err != nil {
		return err
	}

	targetChoiceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Configure target (GitHub with Data Residency) credentials now?").
				Description("Needed for `gh elm target` commands. You can add these later.").
				Value(&configureTarget),
		),
	).WithTheme(theme.Form())
	if err := runForm(targetChoiceForm); err != nil {
		return err
	}

	if configureTarget {
		if err := runConfigCredentialPhase(
			cmd,
			"Target token",
			targetConfigCredential,
			&targetURL,
			&targetToken,
			func() error {
				form := huh.NewForm(
					huh.NewGroup(
						huh.NewNote().
							Title("Target — GitHub with Data Residency").
							Description("The destination the repositories are migrating to."),
						huh.NewInput().
							Title("Target URL").
							Placeholder("https://tenant.ghe.com").
							Value(&targetURL).
							Validate(validateURL),
						huh.NewInput().
							Title("Target token").
							Description("A personal access token with the admin:enterprise and admin:org scopes.").
							EchoMode(huh.EchoModePassword).
							Value(&targetToken).
							Validate(validateRequired("a target token")),
					),
				).WithTheme(theme.Form())
				return runForm(form)
			},
			func() (validationDecision, error) {
				return promptConfigValidationDecision("Target token")
			},
		); err != nil {
			return err
		}
	}

	cfg.SourceURL = strings.TrimSpace(sourceURL)
	if configureTarget {
		cfg.TargetURL = endpoints.NormalizeTargetAPIURL(targetURL)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := store.Set(creds.SourceToken, sourceToken); err != nil {
		return err
	}
	if configureTarget {
		if err := store.Set(creds.TargetToken, targetToken); err != nil {
			return err
		}
	}

	var output bytes.Buffer
	fmt.Fprintln(&output, render.Success("Saved gh elm configuration."))
	fmt.Fprintln(&output, render.Fields(
		render.Field{Label: "Config", Value: configPathOrUnknown()},
		render.Field{Label: "Credentials", Value: store.Location()},
	))
	return render.Write(cmd.OutOrStdout(), output.String())
}

func runSetMigratorPATInteractive(cmd *cobra.Command, org, sourceURL, sourceToken, targetURL string) error {
	//nolint:gosec // os.Stdin.Fd() returns a small, non-negative file descriptor that always fits in an int
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("gh elm config org-tokens is interactive and needs a terminal; " +
			"in non-interactive environments use `config set-source-pat` and `config set-target-pat` " +
			"with the ORG argument and --body or environment variables")
	}

	var newSourceToken, newTargetToken string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Organization Migration Access Tokens").
				Description("The tokens used by the live migration backend. They are stored as secrets on the source GHES appliance and are not stored locally"),
			huh.NewInput().
				Title("SOURCE_PAT").
				Description("A token with access to the source GHES instance. It must have these scopes:\n"+
					"  * repo\n"+
					"  * admin:org\n"+
					"  * admin:repo_hook\n"+
					"  * admin:org_hook").
				EchoMode(huh.EchoModePassword).
				Value(&newSourceToken).
				Validate(validateRequired("a source token")),
			huh.NewInput().
				Title("TARGET_PAT").
				Description("A token with access to the target GitHub instance. It must have these scopes:\n"+
					"  * repo\n"+
					"  * workflow\n"+
					"  * admin:org\n"+
					"  * admin:repo_hook\n"+
					"  * admin:enterprise").
				EchoMode(huh.EchoModePassword).
				Value(&newTargetToken).
				Validate(validateRequired("a target token")),
		),
	).WithTheme(theme.Form())

	if err := runForm(form); err != nil {
		return err
	}

	resolver, err := endpoints.NewResolver()
	if err != nil {
		return err
	}

	ep, err := resolver.Source(sourceURL, sourceToken)
	if err != nil {
		return err
	}

	if ep.URL == "" {
		return fmt.Errorf("no source URL configured; run `gh elm config`, set %s, or pass --source-url", config.EnvSourceURL)
	}
	if ep.Token == "" {
		return fmt.Errorf("no source admin token configured; run `gh elm config`, set %s, or pass --source-token", config.EnvSourceToken)
	}

	targetEP, err := resolver.Target(targetURL, newTargetToken)
	if err != nil {
		return err
	}

	if err := setMigratorPATPair(
		cmd,
		org,
		ep.URL,
		ep.Token,
		targetEP.URL,
		newSourceToken,
		newTargetToken,
		true,
	); err != nil {
		return err
	}

	return render.Write(cmd.OutOrStdout(), render.Success("Set SOURCE_PAT and TARGET_PAT migrator secrets for organization "+org+"."))
}

func setMigratorPATPair(
	cmd *cobra.Command,
	org string,
	sourceAPIURL string,
	sourceAdminToken string,
	targetAPIURL string,
	sourcePAT string,
	targetPAT string,
	interactive bool,
) error {
	sourceResult := validateTokenScopes(cmd.Context(), sourceAPIURL, sourcePAT, sourcePATRequiredScopes)
	targetResult := validateTokenScopes(cmd.Context(), targetAPIURL, targetPAT, targetPATRequiredScopes)

	if err := reportTokenValidation(cmd, "SOURCE_PAT", sourceResult, interactive); err != nil {
		return err
	}
	if err := reportTokenValidation(cmd, "TARGET_PAT", targetResult, interactive); err != nil {
		return err
	}

	client := elmapi.NewClient(sourceAPIURL, sourceAdminToken)
	if err := client.SetMigratorSecret(cmd.Context(), org, "SOURCE_PAT", sourcePAT); err != nil {
		return err
	}
	if err := client.SetMigratorSecret(cmd.Context(), org, "TARGET_PAT", targetPAT); err != nil {
		return fmt.Errorf("SOURCE_PAT was set, but setting TARGET_PAT failed: %w", err)
	}
	return nil
}

func runConfigureShow(cmd *cobra.Command, store creds.Store) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Resolve both token statuses first so a backend read error is reported as
	// an error rather than being masked as "not set" in the printed output.
	sourceToken, err := tokenStatus(store, creds.SourceToken)
	if err != nil {
		return err
	}
	targetToken, err := tokenStatus(store, creds.TargetToken)
	if err != nil {
		return err
	}

	var output bytes.Buffer
	fmt.Fprintln(&output, "Source (GHES):")
	fmt.Fprintln(&output, render.Fields(
		render.Field{Label: "URL", Value: orUnset(cfg.SourceURL)},
		render.Field{Label: "Token", Value: sourceToken},
	))
	fmt.Fprintln(&output, "Target (GitHub with Data Residency):")
	fmt.Fprintln(&output, render.Fields(
		render.Field{Label: "URL", Value: orUnset(cfg.TargetURL)},
		render.Field{Label: "Token", Value: targetToken},
	))
	fmt.Fprintln(&output, "\nStored at:")
	fmt.Fprintln(&output, render.Fields(
		render.Field{Label: "Config", Value: configPathOrUnknown()},
		render.Field{Label: "Credentials", Value: store.Location()},
	))
	return render.Write(cmd.OutOrStdout(), output.String())
}

func runConfigureReset(cmd *cobra.Command) error {
	empty := &config.Config{}
	if err := empty.Save(); err != nil {
		return err
	}
	// Clear both persistent backends so a token written to one (e.g. the
	// keyring) isn't left behind when this run's selected backend is the other.
	if err := creds.ClearAll(creds.SourceToken, creds.TargetToken); err != nil {
		return err
	}
	return render.Write(cmd.OutOrStdout(), render.Success("Cleared gh elm configuration and credentials."))
}

// runForm runs a huh form, translating a user cancellation (Ctrl-C / Esc) into a
// friendly message instead of a raw error.
func runForm(form *huh.Form) error {
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return errors.New("configuration cancelled")
		}
		return err
	}
	return nil
}

func validateRequired(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("please enter %s", what)
		}
		return nil
	}
}

func validateURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("please enter a URL")
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("URL must start with https:// (or http://)")
	}
	if u.Host == "" {
		return errors.New("URL must include a host")
	}
	return nil
}

func orUnset(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func tokenStatus(store creds.Store, key string) (string, error) {
	v, err := store.Get(key)
	if err != nil {
		return "", fmt.Errorf("reading %s token from %s: %w", key, store.Location(), err)
	}
	if v == "" {
		return "not set", nil
	}
	return "set (hidden)", nil
}

func configPathOrUnknown() string {
	p, err := config.Path()
	if err != nil {
		return "(unknown)"
	}
	return p
}
