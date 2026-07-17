// Package cmd assembles the `gh elm` command tree.
package cmd

import (
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
	}

	rootCmd.SetVersionTemplate("gh elm {{.Version}}\n")

	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(migration.NewCommand())
	rootCmd.AddCommand(target.NewCommand())

	return rootCmd
}
