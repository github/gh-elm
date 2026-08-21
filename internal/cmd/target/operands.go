package target

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
)

var canonicalUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type sourceOptions struct {
	url   string
	token string
}

func addSourceFlags(cmd *cobra.Command, source *sourceOptions) {
	cmd.Flags().StringVar(&source.url, "source-url", "", "Override the source (GHES) API base URL when resolving a source migration UUID.")
	cmd.Flags().StringVar(&source.token, "source-token", "", "Override the source (GHES) API token when resolving a source migration UUID.")
}

func resolveTargetMigrationID(
	ctx context.Context,
	positional, flagValue string,
	flagChanged bool,
	source sourceOptions,
) (int64, error) {
	if positional != "" && flagChanged {
		return 0, errors.New("TARGET-MIGRATION-ID cannot be provided both positionally and with --migration-id")
	}
	value := strings.TrimSpace(positional)
	if flagChanged {
		value = strings.TrimSpace(flagValue)
	}
	if value == "" {
		return 0, errors.New("TARGET-MIGRATION-ID is required")
	}

	if targetID, err := strconv.ParseInt(value, 10, 64); err == nil {
		if targetID <= 0 {
			return 0, fmt.Errorf("invalid TARGET-MIGRATION-ID %q: must be a positive integer or canonical UUID", value)
		}
		return targetID, nil
	}
	if !canonicalUUIDPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid TARGET-MIGRATION-ID %q: must be a positive integer or canonical UUID", value)
	}

	resolver, err := endpoints.NewResolver()
	if err != nil {
		return 0, err
	}
	ep, err := resolver.Source(source.url, source.token)
	if err != nil {
		return 0, err
	}
	if ep.URL == "" {
		return 0, fmt.Errorf("resolving source migration UUID %s requires a source URL; run `gh elm config`, set %s, or pass --source-url", value, config.EnvSourceURL)
	}
	if ep.Token == "" {
		return 0, fmt.Errorf("resolving source migration UUID %s requires a source token; run `gh elm config`, set %s, or pass --source-token", value, config.EnvSourceToken)
	}

	client := elmapi.NewClient(ep.URL, ep.Token)
	detail, err := client.GetMigrationDetail(ctx, value)
	if err != nil {
		return 0, annotateSourceLookupError(err, ep.URL)
	}
	if detail.Migration == nil {
		return 0, fmt.Errorf("source migration %s returned no migration record", value)
	}
	if detail.Migration.TargetMigrationID <= 0 {
		return 0, fmt.Errorf("source migration %s does not have a target migration ID yet", value)
	}
	return detail.Migration.TargetMigrationID, nil
}

func annotateSourceLookupError(err error, sourceURL string) error {
	var httpErr *elmapi.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("authentication failed (HTTP %d) for source %s: %s; "+
			"check the source token with `gh elm config show`. Note the %s and %s environment variables override stored config",
			httpErr.StatusCode, sourceURL, httpErr.Message, config.EnvSourceURL, config.EnvSourceToken)
	}
	return err
}

func resolveStringOperand(name, positional, flagName, flagValue string, flagChanged bool) (string, error) {
	positional = strings.TrimSpace(positional)
	flagValue = strings.TrimSpace(flagValue)
	if positional != "" && flagChanged {
		return "", fmt.Errorf("%s cannot be provided both positionally and with --%s", name, flagName)
	}
	if positional != "" {
		return positional, nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	return "", fmt.Errorf("%s is required", name)
}

func resolveAliasedFlag(
	primaryName, primaryValue string,
	primaryChanged bool,
	legacyName, legacyValue string,
	legacyChanged bool,
) (value, flagName string, changed bool, err error) {
	if primaryChanged && legacyChanged {
		return "", "", false, fmt.Errorf("--%s and --%s cannot be used together", primaryName, legacyName)
	}
	if primaryChanged {
		return primaryValue, primaryName, true, nil
	}
	if legacyChanged {
		return legacyValue, legacyName, true, nil
	}
	return "", primaryName, false, nil
}
