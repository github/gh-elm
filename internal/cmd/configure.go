package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/creds"
)

// newConfigureCmd builds the `gh elm configure` command: an interactive setup
// for the source (GHES) and target (GHEC/Proxima) API URLs and tokens.
func newConfigureCmd() *cobra.Command {
	var (
		showFlag  bool
		resetFlag bool
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Interactively set up credentials for gh elm",
		Long: "Configure the source (GHES) and target (GHEC/Proxima) API URLs and tokens that\n" +
			"gh elm uses.\n\n" +
			"URLs are saved to gh-elm's config file; tokens are saved to a separate 0600\n" +
			"credentials file. Environment variables (GHES_URL/GHES_TOKEN,\n" +
			"MIGRATION_TARGET_URL/MIGRATION_TARGET_TOKEN) and command flags override the\n" +
			"stored values, so scripts and CI can skip this command entirely.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := creds.NewStore()
			if err != nil {
				return err
			}
			switch {
			case showFlag:
				return runConfigureShow(cmd, store)
			case resetFlag:
				return runConfigureReset(cmd, store)
			default:
				return runConfigureInteractive(cmd, store)
			}
		},
	}

	cmd.Flags().BoolVar(&showFlag, "show", false, "Print the current configuration (tokens redacted)")
	cmd.Flags().BoolVar(&resetFlag, "reset", false, "Remove stored configuration and credentials")
	cmd.MarkFlagsMutuallyExclusive("show", "reset")

	return cmd
}

func runConfigureInteractive(cmd *cobra.Command, store creds.Store) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("gh elm configure is interactive and needs a terminal; " +
			"in non-interactive environments set GHES_URL/GHES_TOKEN (and " +
			"MIGRATION_TARGET_URL/MIGRATION_TARGET_TOKEN)")
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
				Title("Configure target (GHEC/Proxima) credentials now?").
				Description("Needed for `gh elm target` commands. You can add these later.").
				Value(&configureTarget),
		),
	).WithTheme(huh.ThemeCharm())

	if err := runForm(sourceForm); err != nil {
		return err
	}

	if configureTarget {
		targetForm := huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("Target — GitHub Enterprise Cloud (Proxima)").
					Description("The destination the repositories are migrating to."),
				huh.NewInput().
					Title("Target URL").
					Placeholder("https://api.tenant.ghe.com").
					Value(&targetURL).
					Validate(validateURL),
				huh.NewInput().
					Title("Target token").
					Description("A personal access token for the migration target.").
					EchoMode(huh.EchoModePassword).
					Value(&targetToken).
					Validate(validateRequired("a target token")),
			),
		).WithTheme(huh.ThemeCharm())

		if err := runForm(targetForm); err != nil {
			return err
		}
	}

	cfg.SourceURL = strings.TrimSpace(sourceURL)
	if configureTarget {
		cfg.TargetURL = strings.TrimSpace(targetURL)
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

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Saved gh elm configuration.")
	fmt.Fprintf(out, "  config:      %s\n", configPathOrUnknown())
	fmt.Fprintf(out, "  credentials: %s\n", store.Location())
	return nil
}

func runConfigureShow(cmd *cobra.Command, store creds.Store) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Source (GHES):")
	fmt.Fprintf(out, "  url:   %s\n", orUnset(cfg.SourceURL))
	fmt.Fprintf(out, "  token: %s\n", tokenStatus(store, creds.SourceToken))
	fmt.Fprintln(out, "Target (GHEC/Proxima):")
	fmt.Fprintf(out, "  url:   %s\n", orUnset(cfg.TargetURL))
	fmt.Fprintf(out, "  token: %s\n", tokenStatus(store, creds.TargetToken))
	fmt.Fprintf(out, "\nStored at:\n  config:      %s\n  credentials: %s\n",
		configPathOrUnknown(), store.Location())
	return nil
}

func runConfigureReset(cmd *cobra.Command, store creds.Store) error {
	empty := &config.Config{}
	if err := empty.Save(); err != nil {
		return err
	}
	for _, key := range []string{creds.SourceToken, creds.TargetToken} {
		if err := store.Delete(key); err != nil {
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Cleared gh elm configuration and credentials.")
	return nil
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

func tokenStatus(store creds.Store, key string) string {
	v, err := store.Get(key)
	if err != nil || v == "" {
		return "not set"
	}
	return "set (hidden)"
}

func configPathOrUnknown() string {
	p, err := config.Path()
	if err != nil {
		return "(unknown)"
	}
	return p
}
