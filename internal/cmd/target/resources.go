package target

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/github/gh-elm/internal/config"
	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/endpoints"
	"github.com/github/gh-elm/internal/render"
)

// newResourcesCmd builds `gh elm target resources`, which lists a migration's
// resources from the target (GitHub with Data Residency) REST API. The API exposes these as
// migration nodes — GET /enterprise/migration/:id/nodes.
func newResourcesCmd() *cobra.Command {
	var (
		migrationID int64
		repository  string
		originFlag  string
		stateFlag   string
		maxResults  int
		asJSON      bool
		targetURL   string
		targetToken string
	)

	cmd := &cobra.Command{
		Use:   "resources [TARGET-MIGRATION-ID] [REPOSITORY]",
		Short: "List a migration's resources from the target",
		Long: "List a migration's resources from the target (GitHub with Data Residency) REST API.\n" +
			"A repository filter is currently required. Filter further by state and origin.\n" +
			"When --origin is omitted, resources\n" +
			"from both the backfill and live-update origins are listed.",
		Example: "  gh elm target resources 42 octo-org/octo-repo\n" +
			"  gh elm target resources 42 octo-org/octo-repo --state failed",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var positionalID, positionalRepository string
			argIndex := 0
			if !cmd.Flags().Changed("migration-id") && argIndex < len(args) {
				positionalID = args[argIndex]
				argIndex++
			}
			if !cmd.Flags().Changed("repository") && argIndex < len(args) {
				positionalRepository = args[argIndex]
				argIndex++
			}
			if argIndex < len(args) {
				return errors.New("positional target migration ID or repository duplicates a value already supplied by flag")
			}

			resolvedMigrationID, err := resolveTargetMigrationID(positionalID, migrationID, cmd.Flags().Changed("migration-id"))
			if err != nil {
				return err
			}
			resolvedRepository := positionalRepository
			if resolvedRepository == "" {
				resolvedRepository = repository
			}

			origins, err := resolveOrigins(originFlag)
			if err != nil {
				return err
			}
			state, err := resolveState(stateFlag)
			if err != nil {
				return err
			}

			// TEMPORARY API WORKAROUND: the target list-nodes endpoint currently
			// rejects requests without repository_nwo. Remove this block once the
			// API supports listing resources without a repository filter.
			resolvedRepository = strings.TrimSpace(resolvedRepository)
			if resolvedRepository == "" {
				return errors.New("REPOSITORY is required because the target API currently requires repository_nwo")
			}

			resolver, err := endpoints.NewResolver()
			if err != nil {
				return err
			}
			ep, err := resolver.Target(targetURL, targetToken)
			if err != nil {
				return err
			}
			if ep.URL == "" {
				return fmt.Errorf("no target URL configured; run `gh elm config`, set %s, or pass --target-url", config.EnvTargetURL)
			}
			if ep.Token == "" {
				return fmt.Errorf("no target token configured; run `gh elm config`, set %s, or pass --target-token", config.EnvTargetToken)
			}

			client := elmapi.NewClient(ep.URL, ep.Token)
			out := cmd.OutOrStdout()

			printed := 0
			for _, origin := range origins {
				opts := elmapi.ListNodesOptions{
					RepositoryNWO: resolvedRepository,
					Origin:        origin,
					State:         state,
				}
				for node, err := range client.IterNodes(cmd.Context(), resolvedMigrationID, opts) {
					if err != nil {
						return annotateAuthError(err, ep.URL)
					}
					if maxResults > 0 && printed >= maxResults {
						break
					}
					printed++
					if asJSON {
						if err := printResourceJSON(out, node); err != nil {
							return err
						}
						continue
					}
					printResource(out, node)
				}
				if maxResults > 0 && printed >= maxResults {
					break
				}
			}

			if !asJSON {
				if printed > 0 {
					fmt.Fprintln(out)
				}
				return render.Write(out, resourceCountSummary(printed))
			}
			return nil
		},
	}

	cmd.Flags().Int64VarP(&migrationID, "migration-id", "m", 0, "Target migration ID (alternative to the positional argument).")
	cmd.Flags().StringVarP(&repository, "repository", "R", "", "Repository in owner/repo format (alternative to the positional argument; currently required).")
	cmd.Flags().StringVar(&originFlag, "origin", "", "Filter by origin: backfill or live-update (default: both).")
	cmd.Flags().StringVar(&stateFlag, "state", "", "Filter by state: pending, processed, failed, or eligible (default: all).")
	cmd.Flags().IntVar(&maxResults, "max-results", 0, "Maximum number of resources to return (0 = all).")
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "Output resources as newline-delimited JSON.")
	cmd.Flags().StringVar(&targetURL, "target-url", "", "Override the target API base URL.")
	cmd.Flags().StringVar(&targetToken, "target-token", "", "Override the target API token.")
	return cmd
}

// resolveOrigins maps the --origin flag to the wire values to query. An empty
// flag lists both origins, matching the elm CLI's behavior.
func resolveOrigins(s string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return []string{elmapi.OriginBackfill, elmapi.OriginLiveUpdate}, nil
	case "backfill":
		return []string{elmapi.OriginBackfill}, nil
	case "live_update", "live-update":
		return []string{elmapi.OriginLiveUpdate}, nil
	default:
		return nil, fmt.Errorf("invalid --origin %q: must be backfill or live-update", s)
	}
}

// annotateAuthError turns a 401/403 from the target API into an actionable
// message. Auth failures are commonly caused by a stale GH_TARGET_HOST or
// GH_TARGET_TOKEN environment variable overriding the configured values,
// so we call that out explicitly rather than leaving a bare HTTP error.
func annotateAuthError(err error, targetURL string) error {
	var httpErr *elmapi.HTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
		//nolint:staticcheck // ST1005: intentional multi-line, user-facing CLI error message
		return fmt.Errorf("authentication failed (HTTP %d) for target %s: %s\n"+
			"Check the target token with `gh elm config show`. Note the %s and %s environment variables override stored config.",
			httpErr.StatusCode, targetURL, httpErr.Message, config.EnvTargetURL, config.EnvTargetToken)
	}
	return err
}

// resolveState maps the --state flag to its wire value. An empty flag omits the
// state filter.
func resolveState(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "pending":
		return elmapi.StatePending, nil
	case "processed":
		return elmapi.StateProcessed, nil
	case "failed":
		return elmapi.StateFailed, nil
	case "eligible":
		return elmapi.StateEligible, nil
	default:
		return "", fmt.Errorf("invalid --state %q: must be pending, processed, failed, or eligible", s)
	}
}

func printResource(w io.Writer, n elmapi.Node) {
	parts := make([]string, 0, 4)
	for _, value := range []string{
		friendlyEnum(n.Type, "NODE_TYPE_"),
		friendlyEnum(n.State, "NODE_STATE_"),
		friendlyEnum(n.Origin, "NODE_ORIGIN_"),
		n.ID,
	} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	fmt.Fprintf(w, "• %s\n", strings.Join(parts, " · "))
	if n.Error != "" {
		fmt.Fprintf(w, "  %s\n", render.Warning(n.Error))
	}
}

// printResourceJSON writes a node's raw API JSON as one NDJSON line. Emitting
// the preserved raw bytes (rather than re-marshaling the typed Node) echoes the
// API response verbatim: it keeps fields Node does not model and omits the
// zero-valued fields re-marshaling would otherwise inject.
func printResourceJSON(w io.Writer, n elmapi.Node) error {
	raw := n.Raw
	if len(raw) == 0 {
		// Fallback for nodes that were not decoded from an API response (for
		// example constructed in tests); marshal the typed representation.
		var err error
		raw, err = json.Marshal(n)
		if err != nil {
			return fmt.Errorf("marshaling resource: %w", err)
		}
	}

	// Compact to a single line so multi-line raw JSON stays valid NDJSON.
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return fmt.Errorf("compacting resource JSON: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// friendlyEnum turns a wire enum like NODE_STATE_PENDING into "pending".
func friendlyEnum(s, prefix string) string {
	s = strings.TrimPrefix(s, prefix)
	s = strings.ReplaceAll(s, "_", " ")
	return strings.ToLower(s)
}

func resourceCountSummary(n int) string {
	switch n {
	case 0:
		return "No resources found."
	case 1:
		return "Found 1 resource."
	default:
		return fmt.Sprintf("Found %d resources.", n)
	}
}
