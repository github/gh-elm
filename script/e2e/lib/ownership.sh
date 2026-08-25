#!/usr/bin/env bash
#
# Run-owned migration tracking for the gh-elm control-plane E2E harness.
#
# Cleanup must operate only on migrations created by the current scenario.
# This module records those migration IDs and tracks whether each migration:
#
#   - was explicitly cancelled;
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
declare -A CLEANUP_COMPLETE_MIGRATION_IDS=()

validate_migration_id() {
  local migration_id="$1"

  if [[ -z "$migration_id" ]]; then
    fail \
      "Migration ownership" \
      "Refusing to use an empty migration ID."
  fi

  if [[ "$migration_id" == *$'\n'* || "$migration_id" == *$'\r'* ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not contain line breaks."
  fi

  if [[ "$migration_id" =~ [[:space:]] ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not contain whitespace."
  fi

  if [[ "$migration_id" == -* ]]; then
    fail \
      "Migration ownership" \
      "Migration IDs must not begin with '-'."
  fi
}

migration_is_owned() {
  local migration_id="$1"

  [[ -n "${OWNED_MIGRATION_IDS[$migration_id]:-}" ]]
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

  CANCELLED_MIGRATION_IDS["$migration_id"]=1
  CLEANUP_COMPLETE_MIGRATION_IDS["$migration_id"]=1

  log "Marked migration $migration_id as cancelled and cleanup-complete."
}

mark_cleanup_complete() {
  local migration_id="$1"

  require_owned_migration "$migration_id"

  CLEANUP_COMPLETE_MIGRATION_IDS["$migration_id"]=1

  log "Marked migration $migration_id as cleanup-complete."
}

migration_was_cancelled() {
  local migration_id="$1"

  migration_is_owned "$migration_id" &&
    [[ -n "${CANCELLED_MIGRATION_IDS[$migration_id]:-}" ]]
}

migration_cleanup_is_complete() {
  local migration_id="$1"

  migration_is_owned "$migration_id" &&
    [[ -n "${CLEANUP_COMPLETE_MIGRATION_IDS[$migration_id]:-}" ]]
}

owned_migration_count() {
  printf '%d\n' "${#CREATED_MIGRATION_IDS[@]}"
}
