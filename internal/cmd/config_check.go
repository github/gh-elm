package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/preflight"
)

func newConfigCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check source and target connectivity and authentication",
		Long: "Check network reachability, API service health, and authenticated-user access\n" +
			"for both the configured source and target. All results are written to stderr.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolver, err := endpoints.NewResolver()
			if err != nil {
				return err
			}

			source, sourceResolveErr := resolver.Source("", "")
			target, targetResolveErr := resolver.Target("", "")
			stderr := cmd.ErrOrStderr()
			sourceErr := checkResolvedEndpoint(cmd, stderr, "Source", source.URL, source.Token, sourceResolveErr)
			targetErr := checkResolvedEndpoint(cmd, stderr, "Target", target.URL, target.Token, targetResolveErr)
			return errors.Join(sourceErr, targetErr)
		},
	}
}

func checkResolvedEndpoint(cmd *cobra.Command, stderr io.Writer, name, endpointURL, token string, resolveErr error) error {
	if resolveErr == nil {
		return preflight.CheckEndpoint(cmd.Context(), stderr, name, endpointURL, token)
	}

	fmt.Fprintf(stderr, "[FAIL] %s network: not checked because configuration could not be read\n", name)
	fmt.Fprintf(stderr, "[FAIL] %s service: not checked because configuration could not be read\n", name)
	fmt.Fprintf(stderr, "[FAIL] %s authentication: not checked because configuration could not be read\n", name)
	return fmt.Errorf("%s preflight failed: reading configuration: %w", name, resolveErr)
}
