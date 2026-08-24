package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
		newSetMigratorPATCmd("set-source-pat", "SOURCE_PAT"),
		newSetMigratorPATCmd("set-target-pat", "TARGET_PAT"),
	)

	return cmd
}

func newSetMigratorPATCmd(commandName, secretName string) *cobra.Command {
	var org, sourceURL, sourceToken, body string

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

	sourceForm := huh.NewForm(
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
				Description("A personal access token with the admin:enterprise scope.").
				EchoMode(huh.EchoModePassword).
				Value(&sourceToken).
				Validate(validateRequired("a source token")),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Configure target (GitHub with Data Residency) credentials now?").
				Description("Needed for `gh elm target` commands. You can add these later.").
				Value(&configureTarget),
		),
	).WithTheme(theme.Form())

	if err := runForm(sourceForm); err != nil {
		return err
	}

	if configureTarget {
		targetForm := huh.NewForm(
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
					Description("A personal access token for the migration target.").
					EchoMode(huh.EchoModePassword).
					Value(&targetToken).
					Validate(validateRequired("a target token")),
			),
		).WithTheme(theme.Form())

		if err := runForm(targetForm); err != nil {
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
