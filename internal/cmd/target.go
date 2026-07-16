package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newTargetCmd builds the `gh elm target` command group, which works with
// migration-target (GHEC/Proxima) resources. Subcommands mirror the
// `elm target *` group in elm-exporter (cmd/elm/cmd/target.go).
func newTargetCmd() *cobra.Command {
	targetCmd := &cobra.Command{
		Use:   "target",
		Short: "Work with migration-target (GHEC/Proxima) resources",
		Long: "Read and write migration-target resources (for example nodes and mannequins)\n" +
			"on the GitHub Enterprise Cloud (Proxima) side of a migration.",
		// With no subcommand, show help instead of erroring.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	targetCmd.AddCommand(newTargetPingCmd())

	return targetCmd
}

// newTargetPingCmd is a scaffolding/connectivity check for the target command
// group. It responds "pong" and makes no network calls.
func newTargetPingCmd() *cobra.Command {
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
