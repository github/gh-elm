# gh-elm

A [GitHub CLI](https://cli.github.com) extension for driving **Enterprise Live Migrations (ELM)**
against the GitHub Enterprise Server (GHES) REST API.

`gh elm` is the customer-facing command-line surface for ELM. It talks to the GHES
`/enterprise/live-migrations` REST API with a normal personal access token, so it runs from
your own machine — no SSH into the appliance required. See the design in
[ADR 0007](https://github.com/github/elm-exporter/blob/main/docs/adrs/0007-gh-cli-extension-over-rest.md).

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
gh elm --help            # list available commands
gh elm --version         # print the extension version

gh elm configure         # interactively set up credentials
gh elm configure --show  # print current config (tokens redacted)
gh elm configure --reset # remove stored config and credentials

gh elm migration ping    # scaffolding check for the migration group -> "pong"
gh elm target ping       # scaffolding check for the target group -> "pong"
```

### Command groups

- `gh elm migration ...` — drive the migration lifecycle (create, start, status, list,
  cancel, cutover) against the GHES REST API. Currently: `ping`.
- `gh elm target ...` — read and write migration-target (GHEC/Proxima) resources such as
  nodes and mannequins. Currently: `ping`, `resources`, `create-report`, `report-status`,
  `report-url`.

## Configuration

`gh elm configure` walks you through the two systems `gh elm` talks to:

- **Source** — your GitHub Enterprise Server (GHES) appliance (URL + a PAT with the
  `admin:enterprise` scope).
- **Target** — the GitHub Enterprise Cloud (Proxima) migration target (URL + a PAT). Optional;
  only needed for `gh elm target` commands.

Where values are stored:

- **URLs** (non-secret) → `~/.config/gh-elm/config.json`
- **Tokens** (secret) → the **OS keyring** when one is available (macOS Keychain, Linux Secret
  Service, Windows Credential Manager), otherwise a `~/.config/gh-elm/credentials.json` file
  created `0600`. The file fallback is what's used in keyring-less environments such as
  **Codespaces and CI** (matching how `gh` itself behaves).

Force a specific backend with `GH_ELM_CREDENTIAL_STORE=keyring` or `GH_ELM_CREDENTIAL_STORE=file`.
`gh elm configure --show` prints which backend is active.

### Environment variables and precedence

Every command resolves each URL and token in this order, so scripts and CI can skip
`gh elm configure` entirely:

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
make audit     # fmt + vet + lint + test (what CI runs)
gh elm --version  # verify your local build is installed
```

`make install` registers this directory as the `elm` extension, so `gh elm ...` runs your
local build. Rebuild with `make build` to pick up changes.

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
