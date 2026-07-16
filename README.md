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

gh elm migration ping    # scaffolding check for the migration group -> "pong"
gh elm target ping       # scaffolding check for the target group -> "pong"
```

### Command groups

- `gh elm migration ...` — drive the migration lifecycle (create, start, status, list,
  cancel, cutover) against the GHES REST API. Currently: `ping`.
- `gh elm target ...` — read and write migration-target (GHEC/Proxima) resources such as
  nodes and mannequins. Currently: `ping`.

## Development

Requires [Go](https://go.dev) (version pinned in [`go.mod`](go.mod)) and the `gh` CLI.

```sh
make build     # build the ./gh-elm binary
make install   # build and install this checkout as a local gh extension
make test      # run unit tests (with -race)
make audit     # fmt + vet + test (what CI runs)
gh elm health  # run your local build
```

`make install` registers this directory as the `elm` extension, so `gh elm ...` runs your
local build. Rebuild with `make build` to pick up changes.

## Releasing

Pushing a `v*` tag triggers [`.github/workflows/release.yml`](.github/workflows/release.yml),
which uses [`cli/gh-extension-precompile`](https://github.com/cli/gh-extension-precompile) to
build cross-platform binaries and attach them to a GitHub release. `gh` installs and upgrades
the correct binary for each user's platform.

```sh
git tag v0.1.0
git push origin v0.1.0
```
