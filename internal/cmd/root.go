// Package cmd assembles the `gh elm` command tree.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/cmd/migration"
	"github.com/github/gh-elm/internal/cmd/target"
)

// NewRootCmd builds the root `gh elm` command with all subcommands attached.
// version is injected from main at build time and surfaced via `--version`.
func NewRootCmd(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "elm",
		Short: "Manage Enterprise Live Migrations from your terminal",
		Long: "gh elm is a gh CLI extension for driving Enterprise Live Migrations (ELM)\n" +
			"against the GitHub Enterprise Server REST API.",
		Version: version,
		// The extension formats its own errors in main; keep cobra from
		// double-printing usage and error text on a failed RunE.
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.SetFlagErrorFunc(groupFlagErrorFunc)

	rootCmd.SetVersionTemplate("gh elm {{.Version}}\n")

	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newKitchenSinkCmd())
	rootCmd.AddCommand(migration.NewCommand())
	rootCmd.AddCommand(target.NewCommand())

	return rootCmd
}

// groupFlagErrorFunc converts a flag-parse error into an "unknown command" error
// when a positional token was collected before the bad flag — that token is a
// mistyped subcommand (e.g. `gh elm target repot --migration-id 5`). With no
// positional (e.g. `gh elm --bogus`), the original unknown-flag error stands, so
// a genuine bad flag isn't silently ignored. Set on the root and inherited by
// every subcommand.
func groupFlagErrorFunc(cmd *cobra.Command, err error) error {
	if args := cmd.Flags().Args(); len(args) > 0 {
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}
	return err
}
