// Package target implements the `gh elm target` command group, which works with
// migration-target (GHEC/Proxima) resources.
package target

import (
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	targetCmd.AddCommand(newResourcesCmd())
	targetCmd.AddCommand(newReportCmd())
	targetCmd.AddCommand(newMannequinCmd())
	targetCmd.AddCommand(newMigrationCmd())

	return targetCmd
}
