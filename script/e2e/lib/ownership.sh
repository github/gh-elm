#!/usr/bin/env bash
#
# Run-owned migration tracking for the gh-elm E2E harness.
#
# Cleanup must operate only on migrations created by the current scenario.
# This module records those migration IDs and tracks whether each migration:
#
#   - was explicitly cancelled;
#   - entered cutover and may require a revert;
#   - has completed all required cleanup.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

# Preserve creation order so cleanup runs deterministically.
declare -a CREATED_MIGRATION_IDS=()

# Associative sets keyed by source-side migration UUID.
declare -A OWNED_MIGRATION_IDS=()
declare -A CANCELLED_MIGRATION_IDS=()
declare -A CUTOVER_STARTED_MIGRATION_IDS=()
declare -A CLEANUP_COMPLETE_MIGRATION_IDS=()

validate_migration_id() {
  local migration_id="$1"

  if [[ -z "$migration_id" ]]; then
    fail \
      "Migration ownership" \
      "Refusing to use an empty migration ID."
  fi

  if [[ "$migration_id" == *$'\n'* ||
    "$migration_id" == *$'\r'* ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not contain line breaks."
  fi

  if [[ "$migration_id" =~ [[:space:]] ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not contain whitespace."
  fi

  if [[ "$migration_id" =~ [[:cntrl:]] ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not contain control characters."
  fi

  if [[ "$migration_id" == -* ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not begin with '-'."
  fi
}

migration_is_owned() {
  local migration_id="$1"

  [[ -n "$migration_id" &&
    -n "${OWNED_MIGRATION_IDS[$migration_id]:-}" ]]
}

require_owned_migration() {
  local migration_id="$1"

  validate_migration_id "$migration_id"

  if ! migration_is_owned "$migration_id"; then
    fail \
      "Migration ownership" \
      "Refusing to modify migration $migration_id because it was not created by this E2E scenario."
  fi
}

remember_migration() {
  local migration_id="$1"

  validate_migration_id "$migration_id"

  if [[ -z "${MIGRATIONS_FILE:-}" ]]; then
    fail \
      "Harness" \
      "Migration ownership storage is unavailable; initialize_harness must run first."
  fi

  if migration_is_owned "$migration_id"; then
    fail \
      "Migration ownership" \
      "Migration $migration_id was recorded more than once."
  fi

  # Register ownership in memory before writing evidence. If the evidence
  # append fails, the EXIT trap must still be able to clean up the remote
  # migration created by this process.
  OWNED_MIGRATION_IDS["$migration_id"]=1
  CREATED_MIGRATION_IDS+=("$migration_id")

  if ! printf '%s\n' "$migration_id" >>"$MIGRATIONS_FILE"; then
    fail \
      "Migration ownership" \
      "Failed to persist ownership evidence for migration $migration_id."
  fi

  log "Recorded run-owned migration: $migration_id"
}

mark_cancelled() {
  local migration_id="$1"

  require_owned_migration "$migration_id"

  if migration_started_cutover "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cannot mark migration $migration_id as cancelled because this scenario recorded a cutover attempt."
  fi

  if migration_cleanup_is_complete "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cannot mark migration $migration_id as cancelled because cleanup is already complete."
  fi

  CANCELLED_MIGRATION_IDS["$migration_id"]=1
  CLEANUP_COMPLETE_MIGRATION_IDS["$migration_id"]=1

  log "Marked migration $migration_id as cancelled and cleanup-complete."
}

mark_cutover_started() {
  local migration_id="$1"

  require_owned_migration "$migration_id"

  if migration_was_cancelled "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cannot mark cutover as started for cancelled migration $migration_id."
  fi

  if migration_cleanup_is_complete "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cannot mark cutover as started for cleanup-complete migration $migration_id."
  fi

  if migration_started_cutover "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cutover was recorded more than once for migration $migration_id."
  fi

  # Record this before the remote cutover request is sent. If the process is
  # interrupted after the service accepts the request, cleanup will know that
  # cancellation is insufficient and that a cutover revert must be attempted.
  CUTOVER_STARTED_MIGRATION_IDS["$migration_id"]=1

  log "Marked migration $migration_id as having started cutover."
}

mark_cleanup_complete() {
  local migration_id="$1"

  require_owned_migration "$migration_id"

  if migration_cleanup_is_complete "$migration_id"; then
    fail \
      "Migration ownership" \
      "Cleanup was recorded more than once for migration $migration_id."
  fi

  CLEANUP_COMPLETE_MIGRATION_IDS["$migration_id"]=1

  log "Marked migration $migration_id as cleanup-complete."
}

migration_was_cancelled() {
  local migration_id="$1"

  migration_is_owned "$migration_id" &&
    [[ -n "${CANCELLED_MIGRATION_IDS[$migration_id]:-}" ]]
}

migration_started_cutover() {
  local migration_id="$1"

  migration_is_owned "$migration_id" &&
    [[ -n "${CUTOVER_STARTED_MIGRATION_IDS[$migration_id]:-}" ]]
}

migration_cleanup_is_complete() {
  local migration_id="$1"

  migration_is_owned "$migration_id" &&
    [[ -n "${CLEANUP_COMPLETE_MIGRATION_IDS[$migration_id]:-}" ]]
}

owned_migration_count() {
  printf '%d\n' "${#CREATED_MIGRATION_IDS[@]}"
}
