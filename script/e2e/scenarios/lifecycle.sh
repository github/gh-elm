#!/usr/bin/env bash
#
# Full migration lifecycle scenario for the gh-elm E2E harness.
#
# This scenario exercises creation, start, target-ID resolution, target
# resources, cutover, cutover completion, revert, and post-revert status
# verification.
#
# Pause and resume are intentionally excluded because the protected GHES
# sandbox uses the database-backed work scheduler, which does not currently
# support those operations.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and defines the required
# run_scenario function. It is not intended to be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

run_scenario() {
  local migration_id
  local target_migration_id

  log "Starting the lifecycle E2E scenario."

  create_migration \
    "$TARGET_REPO_PRIMARY" \
    "Create lifecycle migration"
  migration_id="$LAST_MIGRATION_ID"

  verify_status \
    "$migration_id" \
    "$TARGET_REPO_PRIMARY" \
    "Initial migration status"

  start_migration \
    "$migration_id" \
    "Start migration"

  target_migration_id="$(
    resolve_target_migration_id \
      "$migration_id" \
      "Target migration ID"
  )"

  verify_target_migration_id \
    "$target_migration_id" \
    "Target migration ID validation"

  wait_for_cutover_readiness \
    "$migration_id" \
    "$E2E_CUTOVER_TIMEOUT_SECONDS" \
    "Wait for cutover readiness"

  verify_target_resources \
    "$target_migration_id" \
    "$TARGET_ORG/$TARGET_REPO_PRIMARY" \
    "Target resources"

  initiate_cutover \
    "$migration_id" \
    "Initiate cutover"

  wait_for_cutover_completion \
    "$migration_id" \
    "$E2E_CUTOVER_TIMEOUT_SECONDS" \
    "Wait for cutover completion"

  revert_cutover \
    "$migration_id" \
    "Revert cutover"

  verify_reverted_state \
    "$migration_id" \
    "$TARGET_REPO_PRIMARY" \
    "Verify reverted state"

  log "Lifecycle assertions completed."
}
