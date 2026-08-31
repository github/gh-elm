#!/usr/bin/env bash
#
# Shared defaults, state, logging, and validation primitives for the gh-elm
# control-plane E2E harness.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

# Prevent accidental direct execution. BASH_SOURCE[0] differs from $0 when this
# file is sourced.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Harness configuration
# ---------------------------------------------------------------------------

OUTDIR="${OUTDIR:-./elm-results}"
E2E_MODE="${E2E_MODE:-control-plane}"

# ---------------------------------------------------------------------------
# Evidence paths
#
# These are populated by initialize_harness() in evidence.sh after OUTDIR has
# been created.
# ---------------------------------------------------------------------------

EVIDENCE_FILE=""
RESULTS_FILE=""
MIGRATIONS_FILE=""
CLEANUP_LOG=""
COMMAND_LOG=""
METADATA_FILE=""

# ---------------------------------------------------------------------------
# Runtime state
#
# These are populated by configure_runtime() in configuration.sh.
# ---------------------------------------------------------------------------

SAFE_RUN_ID=""
TARGET_REPO_PRIMARY=""
TARGET_REPO_PAGINATION=""
LAST_MIGRATION_ID=""

# ---------------------------------------------------------------------------
# Logging and failure handling
# ---------------------------------------------------------------------------

log() {
  printf '%s\n' "$*"
}

log_error() {
  printf 'ERROR: %s\n' "$*" >&2
}

fail() {
  local key="$1"
  local note="$2"

  # record_result is defined in evidence.sh. Avoid obscuring the original
  # failure if initialization failed before evidence.sh created its files.
  if declare -F record_result >/dev/null 2>&1 &&
    [[ -n "${RESULTS_FILE:-}" ]] &&
    [[ -n "${EVIDENCE_FILE:-}" ]]; then
    record_result "$key" "❌ fail" "$note"
  else
    log_error "$key: $note"
  fi

  exit 1
}

# ---------------------------------------------------------------------------
# Generic validation primitives
# ---------------------------------------------------------------------------

require_variable() {
  local name="$1"

  if [[ -z "${!name:-}" ]]; then
    fail \
      "Configuration" \
      "Required environment variable $name is missing."
  fi
}

require_command() {
  local name="$1"

  if ! command -v "$name" >/dev/null 2>&1; then
    fail \
      "Dependencies" \
      "Required command $name was not found on PATH."
  fi
}
