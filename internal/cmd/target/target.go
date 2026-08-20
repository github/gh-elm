// Package target implements commands that operate on the destination side of a
// migration.
package target

import (
	"github.com/spf13/cobra"
)

// NewCommand builds the `gh elm target` command group. Subcommands mirror the
// `elm target *` group in elm-exporter (cmd/elm/cmd/target.go).
func NewCommand() *cobra.Command {
	targetCmd := &cobra.Command{
		Use:   "target",
		Short: "Work with destination migration data",
		Long: "Inspect resources and reports, and reclaim mannequins, on the destination\n" +
			"(GitHub with Data Residency) side of a migration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	targetCmd.AddCommand(newResourcesCmd())
	targetCmd.AddCommand(newReportCmd())
	targetCmd.AddCommand(newMannequinCmd())

	return targetCmd
}
