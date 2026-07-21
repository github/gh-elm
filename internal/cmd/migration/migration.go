// Package migration implements the `gh elm migration` command group, which
// drives the migration lifecycle against the source (GHES) REST API.
package migration

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand builds the `gh elm migration` command group. Subcommands mirror the
// `elm migration *` group in elm-exporter (cmd/elm/cmd/migration.go).
func NewCommand() *cobra.Command {
	migrationCmd := &cobra.Command{
		Use:   "migration",
		Short: "Manage migrations via the GHES REST API",
		Long: "Create, start, monitor, and control Enterprise Live Migrations through the\n" +
			"GitHub Enterprise Server REST API.",
		// No subcommand shows help; an unknown subcommand (e.g. a typo) fails
		// via NoArgs rather than being swallowed as a positional argument.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	migrationCmd.AddCommand(newPingCmd())

	return migrationCmd
}

// newPingCmd is a scaffolding/connectivity check for the migration command
// group. It responds "pong" and makes no network calls.
func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check that the migration command group is wired up",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "pong")
			return err
		},
	}
}
