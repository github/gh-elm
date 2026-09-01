#!/usr/bin/env bash
#
# Migration control-plane scenario for the gh-elm E2E harness.
#
# This scenario exercises migration creation, status retrieval, list
# pagination, explicit cancellation, and automatic run-owned cleanup.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and defines the required
# run_scenario function. It is not intended to be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

run_scenario() {
  local primary_id
  local pagination_id

  log "Starting the control-plane E2E scenario."

  create_migration \
    "$TARGET_REPO_PRIMARY" \
    "Create primary migration"
  primary_id="$LAST_MIGRATION_ID"

  verify_status \
    "$primary_id" \
    "$TARGET_REPO_PRIMARY" \
    "Migration status"

  # Keep both migrations in the created state so they remain eligible for the
  # created-status pagination query.
  create_migration \
    "$TARGET_REPO_PAGINATION" \
    "Create pagination migration"
  pagination_id="$LAST_MIGRATION_ID"

  verify_pagination \
    "$primary_id" \
    "$pagination_id"

  cancel_migration \
    "$primary_id" \
    "Cancel primary migration"

  # Leave the pagination migration active so the EXIT trap exercises automatic
  # run-owned cleanup. Cleanup may operate only on IDs recorded by this
  # scenario.
  log "Control-plane assertions completed."
}
