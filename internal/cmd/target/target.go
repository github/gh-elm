// Package target implements the `gh elm target` command group, which works with
// migration-target (GHEC/Proxima) resources.
package target

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand builds the `gh elm target` command group. Subcommands mirror the
// `elm target *` group in elm-exporter (cmd/elm/cmd/target.go).
func NewCommand() *cobra.Command {
	targetCmd := &cobra.Command{
		Use:   "target",
		Short: "Work with migration-target (GHEC/Proxima) resources",
		Long: "Read and write migration-target resources (for example migration resources\n" +
			"and mannequins) on the GitHub Enterprise Cloud (Proxima) side of a migration.",
		// No subcommand shows help; an unknown subcommand (e.g. a typo) fails
		// via NoArgs rather than being swallowed as a positional argument.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	targetCmd.AddCommand(newPingCmd())
	targetCmd.AddCommand(newResourcesCmd())
	targetCmd.AddCommand(newCreateReportCmd())
	targetCmd.AddCommand(newReportStatusCmd())
	targetCmd.AddCommand(newReportURLCmd())
	targetCmd.AddCommand(newMannequinCmd())

	return targetCmd
}

// newPingCmd is a scaffolding/connectivity check for the target command group.
// It responds "pong" and makes no network calls.
func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check that the target command group is wired up",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "pong")
			return err
		},
	}
}
