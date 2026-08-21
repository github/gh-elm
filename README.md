# gh-elm

A [GitHub CLI](https://cli.github.com) extension for driving **Enterprise Live Migrations (ELM)**
against the GitHub Enterprise Server (GHES) REST API.

`gh elm` is the customer-facing command-line surface for ELM. It talks to the GHES
`/enterprise/live-migrations` REST API with a normal personal access token, so it runs from
your own machine — no SSH into the appliance required.

## Project status

`gh-elm` is under active development and maintained by GitHub's migrations team.
External contributions are welcome.

The extension is focused on Enterprise Live Migrations from GitHub Enterprise Server. It
requires a GHES version that provides the ELM REST API and is not a replacement for other
GitHub migration tools.

## Installation

```sh
gh extension install github/gh-elm
```

Upgrade later with:

```sh
gh extension upgrade elm
```

## Usage

```sh
gh elm                   # launch the interactive terminal UI
gh elm --help            # list available commands
gh elm --version         # print the extension version

gh elm config       # interactively set up credentials
gh elm config show  # print current config (tokens redacted)
gh elm config reset # remove stored config and credentials
```

When standard input and output are interactive terminals, invoking `gh elm` with no
arguments opens a full-screen TUI. Its main migration workflow supports creating,
monitoring, and controlling migrations, then links to destination details, resources,
and reports without repeatedly copying IDs. Lower-level destination migration controls
remain available under **Advanced destination operations**. Use arrow keys or `j`/`k`
to move, Enter to select, Escape to go back, `/` to search migrations, `Ctrl+V` to
toggle list density, `?` for contextual help, and `q` to quit. Long detail and result
views support Page Up and Page Down.

Migration creation lazily loads searchable source repositories and destination
organizations from the configured APIs. The destination repository name defaults to
the selected source name and remains editable. Press `Ctrl+E` from either picker to
fall back to manual `org/repo` entry.

In scripts, redirected output, and other non-interactive environments, bare `gh elm`
continues to print help and exit successfully. Explicit commands and machine-readable
formats remain the supported automation interface.

### Command groups

- `gh elm migration ...` — drive the migration lifecycle (create, start, status, list,
  cancel, cutover) against the GHES REST API, plus `target-id` to resolve a
  migration's destination (GitHub with Data Residency) migration ID for use with the
  `gh elm target *` commands. Create, status, list, and cutover-revert output is
  human-readable by default; add `--json` for the raw API response.
- `gh elm target migration list|create|status|pause|resume|abort` — manage a migration
  record directly on the target (GitHub with Data Residency) side. Most workflows only need `list` and
  `status`; `create`/`pause`/`resume`/`abort` are lower-level operations for debugging or
  advanced workflows, since `gh elm migration create` normally drives the target record from
  the source side.
- `gh elm target report request|status|url` — request a migration's node report, poll its
  status, and get a signed download URL. Human-readable by default; add `--json` for the
  raw API response.
- `gh elm target mannequin list|reclaim` — manage mannequins for a target organization.
  Use `--output` on `list` to save a CSV, then edit and pass it to `reclaim --csv`.

### Examples

Resolve the target endpoint via `gh elm config` (or `GH_TARGET_HOST` /
`GH_TARGET_TOKEN`); every command also accepts per-invocation `--target-url` and
`--target-token` overrides.

Migration responses are rendered for people by default:

```sh
# Create a migration using concise repository coordinates and show its ID and expiry
gh elm migration create source-org/repo target-org/repo

# List in-progress migrations, falling back to created migrations when empty
gh elm migration list

# Migration commands accept the ID positionally (-m/--migration-id remains supported)
gh elm migration cancel <uuid>

# Show formatted status details or list migrations
gh elm migration status <uuid>
gh elm migration list --status all

# Preserve the raw API response for scripts and jq
gh elm migration status <uuid> --json | jq .combined_state.status
gh elm migration list --status all --json | jq '.migrations[]'

# Initiate cutover
gh elm migration cutover <uuid>

# Inspect or revert cutover
gh elm migration cutover status <uuid>
gh elm migration cutover revert <uuid>
gh elm migration cutover revert <uuid> --json | jq .success
```

Look up a migration's destination (GitHub with Data Residency) migration ID —
`gh elm migration target-id` (human-readable by default; add `--json` for a
machine-readable object). The numeric target migration ID it returns is the positional
`TARGET-MIGRATION-ID` accepted by `gh elm target *` commands, so use this to bridge from a
migration's source UUID to the target ID those commands need:

```sh
# Human-readable
gh elm migration target-id <uuid>

# Machine-readable, e.g. piped to jq
gh elm migration target-id <uuid> --json | jq .target_migration_id

# Resolve the target ID and feed it straight into a target command
TARGET_ID=$(gh elm migration target-id <uuid> --json | jq .target_migration_id)
gh elm target resources "$TARGET_ID" octo-org/octo-repo
```

Migration resources — `gh elm target resources`:

```sh
# Resources for a repository (both backfill and live-update origins)
gh elm target resources 42 octo-org/octo-repo

# Filter further by origin and state
gh elm target resources 42 octo-org/octo-repo --origin backfill
gh elm target resources 42 octo-org/octo-repo --state failed

# Cap results and emit newline-delimited JSON for scripting
gh elm target resources 42 octo-org/octo-repo --max-results 20
gh elm target resources 42 octo-org/octo-repo --json | jq -s 'group_by(.type) | map({type: .[0].type, count: length})'
```

Managing a migration on the target — `gh elm target migration`:

```sh
# List migrations (human-readable by default; add --json for newline-delimited JSON)
gh elm target migration list
gh elm target migration list --status paused

# Create a migration directly on the target (single repository per migration)
gh elm target migration create --source-repository-url https://ghes.example/octo-org/octo-repo \
  --repository octo-org/octo-repo --description "manual test migration"

# Check status and per-repository progress
gh elm target migration status --migration-id 42

# Pause, resume, or abort
gh elm target migration pause --migration-id 42
gh elm target migration resume --migration-id 42
gh elm target migration abort --migration-id 42
```

Node reports — `gh elm target report request|status|url` (human-readable by default; add
`--json` for the raw API response):

```sh
# Request a backfill report over all nodes (default --state all)
gh elm target report request 42 --stage backfill

# Request a live-updates report over only unmigrated nodes
gh elm target report request 42 --stage live-update --state unmigrated

# Poll status (human-readable), or as raw JSON piped to jq
gh elm target report status 42 --stage backfill
gh elm target report status 42 --stage backfill --json | jq -r .status

# Grab the signed download URL and fetch the finished archive
URL=$(gh elm target report url 42 --stage backfill --json | jq -r .url)
curl -sSL "$URL" -o report.zip
```

Mannequins — `gh elm target mannequin list` / `reclaim`:

```sh
# List unclaimed mannequins to stdout, or all of them to a file
gh elm target mannequin list octo-org
gh elm target mannequin list octo-org --include-reclaimed --output mannequins.csv

# Reclaim a single mannequin via the invitation email flow
gh elm target mannequin reclaim octo-org octocat real-user

# Claim in bulk from an edited CSV
gh elm target mannequin reclaim octo-org --csv mannequins.csv

# Immediate reattribution (EMU orgs only); prompts unless --no-prompt
gh elm target mannequin reclaim octo-org --csv mannequins.csv --skip-invitation
```

## Configuration

`gh elm config` walks you through the two systems `gh elm` talks to:

- **Source** — your GitHub Enterprise Server (GHES) appliance (URL + a PAT with the
  `admin:enterprise` scope).
- **Target** — the GitHub with Data Residency migration target (URL + a PAT). Optional;
  only needed for `gh elm target` commands.

Where values are stored:

- **URLs** (non-secret) → `~/.config/gh-elm/config.json`
- **Tokens** (secret) → the **OS keyring** when one is available (macOS Keychain, Linux Secret
  Service, Windows Credential Manager), otherwise a `~/.config/gh-elm/credentials.json` file
  created `0600`. The file fallback is what's used in keyring-less environments such as
  **Codespaces and CI** (matching how `gh` itself behaves).

Force a specific backend with `GH_ELM_CREDENTIAL_STORE=keyring` or `GH_ELM_CREDENTIAL_STORE=file`.
`gh elm config show` prints which backend is active.

### Environment variables and precedence

Every command resolves each URL and token in this order, so scripts and CI can skip
`gh elm config` entirely:

```
--flag  >  environment variable  >  stored config/credentials
```

The environment variables use a unified `GH_SOURCE_*` / `GH_TARGET_*` scheme:

| Purpose | Env var |
| --- | --- |
| Source host | `GH_SOURCE_HOST` |
| Source token | `GH_SOURCE_TOKEN` |
| Target host | `GH_TARGET_HOST` |
| Target token | `GH_TARGET_TOKEN` |

Other overrides:

- `GH_ELM_CONFIG_DIR` — config directory (otherwise follows `GH_CONFIG_DIR` / `XDG_CONFIG_HOME`).
- `GH_ELM_CREDENTIAL_STORE` — force the token backend: `keyring` or `file`.

## Development

Requires [Go](https://go.dev) (version pinned in [`go.mod`](go.mod)) and the `gh` CLI.

```sh
make build     # build the ./gh-elm binary
make install   # build and install this checkout as a local gh extension
make test      # run unit tests (with -race)
make test-integration  # build and exercise gh-elm as a real subprocess
make audit     # run formatting, vet, lint, unit, and integration tests
gh elm --version  # verify your local build is installed
```

`make install` registers this directory as the `elm` extension, so `gh elm ...` runs your
local build. Rebuild with `make build` to pick up changes.

Integration tests build a temporary `gh-elm` executable and run it as a real
subprocess against local HTTP test servers. They use isolated configuration and
a file-backed credential store, require no GitHub credentials, and do not
contact live GHES or GitHub with Data Residency environments.

CI runs these tests natively on Linux, macOS, and Windows.

## Releasing

Releases are drafted automatically and built with GoReleaser:

1. On every merge to `main`, [`release-drafter`](https://github.com/release-drafter/release-drafter)
   ([`.github/workflows/release-drafter.yml`](.github/workflows/release-drafter.yml)) keeps a
   **draft** GitHub Release up to date. The next version is derived from PR labels
   (`major`/`minor`/`patch`, plus `feature`, `fix`, etc.) and the changelog from merged PR
   titles (see [`.github/release-drafter.yml`](.github/release-drafter.yml)). PRs are
   auto-labelled from their branch name and title (e.g. `feat/…`, `fix:`), so labels usually
   don't need to be applied by hand.
2. When you're ready to ship, **publish** the draft release from the GitHub UI. Publishing
   creates the `v*` tag.
3. The tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml), which runs
   [GoReleaser](https://goreleaser.com) ([`.goreleaser.yaml`](.goreleaser.yaml)) to build
   cross-platform binaries and attach them to that release. `gh` installs and upgrades the
   correct binary for each user's platform.

The published binaries are named `gh-elm_<version>_<os>-<arch>` so that
`gh extension install github/gh-elm` and `gh extension upgrade` can pick the right asset.

## Contributing

External contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for development
requirements and pull request guidance.

## Maintainers

See [CODEOWNERS](CODEOWNERS).

## Support

See [SUPPORT.md](SUPPORT.md) for support channels and expectations.

## License

This project is licensed under the terms of the MIT open source license. See
[LICENSE](LICENSE) for the full terms.
