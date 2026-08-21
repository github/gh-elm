// Command kitchen-sink previews every migration response renderer with dummy
// API responses. It is a developer tool and is not part of the gh extension
// command tree.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/github/gh-elm/internal/elmapi"
	"github.com/github/gh-elm/internal/render"
)

var fixtures = []struct {
	name   string
	raw    json.RawMessage
	render func(json.RawMessage) (string, error)
}{
	{
		name: "gh elm migration create",
		raw: json.RawMessage(`{
  "migration_id": "7e3f16ca-44da-4f9a-a806-2c798a6afda7",
  "expires_at": "2026-08-13T10:00:00Z"
}`),
		render: renderCreate,
	},
	{
		name: "gh elm migration status",
		raw: json.RawMessage(`{
  "migration": {
    "migration_id": "7e3f16ca-44da-4f9a-a806-2c798a6afda7",
    "status": "in_progress",
    "source_organization_login": "source-org",
    "source_repository_name": "monolith",
    "target_organization_login": "target-org",
    "target_repository_name": "monolith",
    "target_visibility": "internal",
    "target_migration_id": 4242,
    "created_at": "2026-08-06T08:00:00Z",
    "started_at": "2026-08-06T08:05:00Z",
    "completed_at": null,
    "expires_at": "2026-08-13T10:00:00Z"
  },
  "target_state": {
    "status": "in_progress",
    "target_unavailable": false,
    "repository_progress": [
      {
        "repository_nwo": "target-org/monolith",
        "backfill_resources_added": 1250,
        "backfill_resources_processed": 1100,
        "backfill_resources_failed": 3,
        "live_update_resources_added": 84,
        "live_update_resources_processed": 79,
        "live_update_resources_failed": 1,
        "all_resources_sent": false,
        "initial_git_push_complete": true,
        "repository_locked": false
      }
    ]
  },
  "combined_state": {
    "status": "processing",
    "display_message": "Backfill is still processing resources.",
    "ready_for_cutover": false,
    "cutover_blockers": [
      "Backfill has not completed",
      "Four resources require attention"
    ],
    "repositories": [
      {
        "repository_nwo": "target-org/monolith",
        "phase": "backfill",
        "display_status": "1,100 of 1,250 resources processed"
      }
    ]
  },
  "messages": [
    {
      "message_type": "success",
      "message": "[preflight: repository access] Passed",
      "created_at": "2026-08-06T08:04:00Z"
    },
    {
      "message_type": "warning",
      "message": "Live updates are falling behind.",
      "created_at": "2026-08-06T09:30:00Z"
    },
    {
      "message_type": "error",
      "message": "Three backfill resources failed and should be reviewed.",
      "created_at": "2026-08-06T09:42:00Z"
    }
  ]
}`),
		render: renderStatus,
	},
	{
		name: "gh elm migration list",
		raw: json.RawMessage(`{
  "migrations": [
    {
      "migration_id": "7e3f16ca-44da-4f9a-a806-2c798a6afda7",
      "status": "in_progress",
      "source_organization_login": "source-org",
      "source_repository_name": "monolith",
      "target_organization_login": "target-org",
      "target_repository_name": "monolith",
      "target_visibility": "internal",
      "target_migration_id": 4242,
      "created_at": "2026-08-06T08:00:00Z",
      "started_at": "2026-08-06T08:05:00Z",
      "completed_at": null,
      "expires_at": "2026-08-13T10:00:00Z"
    },
    {
      "migration_id": "c7fd8baa-8680-43c6-8286-c2409b2b3f51",
      "status": "completed",
      "source_organization_login": "source-org",
      "source_repository_name": "api",
      "target_organization_login": "target-org",
      "target_repository_name": "api",
      "target_visibility": "private",
      "target_migration_id": 4188,
      "created_at": "2026-08-05T13:00:00Z",
      "started_at": "2026-08-05T13:04:00Z",
      "completed_at": "2026-08-05T15:31:00Z",
      "expires_at": "2026-08-12T13:00:00Z"
    },
    {
      "migration_id": "11ef6308-489c-466f-a755-5196f809bcc9",
      "status": "failed",
      "source_organization_login": "source-org",
      "source_repository_name": "web",
      "target_organization_login": "target-org",
      "target_repository_name": "web",
      "target_visibility": "internal",
      "target_migration_id": 4101,
      "created_at": "2026-08-04T09:00:00Z",
      "started_at": "2026-08-04T09:02:00Z",
      "completed_at": null,
      "expires_at": "2026-08-11T09:00:00Z"
    }
  ],
  "total_count": 7,
  "next_cursor": "eyJtaWdyYXRpb25faWQiOiIxMWVmNjMwOCJ9"
}`),
		render: renderList,
	},
	{
		name: "gh elm migration cutover revert",
		raw: json.RawMessage(`{
  "success": true,
  "unarchived_source_repository": true,
  "in_progress_cutover_terminated": true,
  "in_progress_migration_terminated": false
}`),
		render: renderRevertCutover,
	},
}

func main() {
	asJSON := flag.Bool("json", false, "Show formatted JSON responses instead of human-readable output.")
	forceColor := flag.Bool("color", false, "Force ANSI color even when output is captured.")
	flag.Parse()

	if *forceColor {
		lipgloss.SetColorProfile(termenv.ANSI256)
	}
	if err := run(os.Stdout, *asJSON); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer, asJSON bool) error {
	for i, fixture := range fixtures {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "=== %s ===\n\n", fixture.name); err != nil {
			return err
		}
		if asJSON {
			if err := render.WriteRawJSON(out, fixture.raw); err != nil {
				return err
			}
			continue
		}

		document, err := fixture.render(fixture.raw)
		if err != nil {
			return fmt.Errorf("rendering %s: %w", fixture.name, err)
		}
		if err := render.Write(out, document); err != nil {
			return err
		}
	}
	return nil
}

func renderCreate(raw json.RawMessage) (string, error) {
	var response elmapi.CreateMigrationResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	return render.MigrationCreate(response), nil
}

func renderStatus(raw json.RawMessage) (string, error) {
	var response elmapi.MigrationDetail
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	return render.MigrationStatus(response), nil
}

func renderList(raw json.RawMessage) (string, error) {
	var response elmapi.ListMigrationsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	return render.MigrationList(response), nil
}

func renderRevertCutover(raw json.RawMessage) (string, error) {
	var response elmapi.RevertCutoverResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	return render.MigrationRevertCutover(response), nil
}
