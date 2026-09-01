#!/usr/bin/env bash
#
# Run-scoped cleanup and final result reporting for the gh-elm E2E harness.
#
# Cleanup operates exclusively on migration IDs recorded by ownership.sh:
#
#   - migrations already marked cleanup-complete are skipped;
#   - migrations that may have entered cutover must be successfully reverted;
#   - other active migrations are cancelled;
#   - rejected cancellation operations are accepted only when status confirms
#     that a non-cutover migration is already terminal.
#
# The cleanup function is installed as an EXIT trap by test-elm-ghes.sh. It
# preserves the scenario's original exit status unless cleanup itself fails.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

cleanup_log() {
  if [[ -n "${CLEANUP_LOG:-}" ]]; then
    printf '%s\n' "$*" >>"$CLEANUP_LOG"
  else
    printf '%s\n' "$*" >&2
  fi
}

cleanup_status_is_terminal() {
  local status="$1"

  case "$status" in
    completed | failed | terminated | cancelled | canceled | aborted | expired)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

cleanup_get_migration_status() {
  local migration_id="$1"

  # Assign the fetched response to the variable named by the second argument.
  # The fetched value deliberately uses a different local name so the nameref
  # cannot resolve to a local variable in this function when callers pass a
  # variable named "response".
  local -n response_ref="$2"
  local fetched_response
  local artifact_id
  local invalid_status_file
  local status_file
  local temporary_file

  if ! migration_is_owned "$migration_id"; then
    cleanup_log \
      "Refusing to retrieve cleanup status for unowned migration $migration_id."

    return 1
  fi

  if ! fetched_response="$(
    gh elm migration status \
      --migration-id "$migration_id" \
      --json 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to retrieve status for migration $migration_id during cleanup."

    return 1
  fi

  artifact_id="$(migration_artifact_id "$migration_id")"

  if ! jq -e \
    --arg migration_id "$migration_id" '
      (.migration | type == "object") and
      .migration.migration_id == $migration_id and
      (
        .migration.status |
        type == "string" and length > 0
      ) and
      (
        .combined_state == null or
        (.combined_state | type == "object")
      ) and
      (
        .combined_state == null or
        .combined_state.status == null or
        (.combined_state.status | type == "string")
      )
    ' >/dev/null <<<"$fetched_response" 2>>"$CLEANUP_LOG"; then
    cleanup_log \
      "Migration $migration_id returned an unexpected status response during cleanup."

    invalid_status_file="$OUTDIR/cleanup-invalid-status-${artifact_id}.json"

    if ! printf '%s\n' "$fetched_response" >"$invalid_status_file"; then
      cleanup_log \
        "Failed to save the invalid cleanup status response for migration $migration_id."
    fi

    return 1
  fi

  status_file="$OUTDIR/cleanup-status-${artifact_id}.json"
  temporary_file="${status_file}.tmp"

  # Save the validated response atomically so interruption cannot leave a
  # partially written JSON artifact.
  if ! printf '%s\n' "$fetched_response" >"$temporary_file"; then
    rm -f "$temporary_file"

    cleanup_log \
      "Failed to save cleanup status evidence for migration $migration_id."

    return 1
  fi

  if ! mv "$temporary_file" "$status_file"; then
    rm -f "$temporary_file"

    cleanup_log \
      "Failed to finalize cleanup status evidence for migration $migration_id."

    return 1
  fi

  response_ref="$fetched_response"
}

cleanup_migration_is_terminal() {
  local migration_id="$1"
  local response
  local migration_status
  local combined_status

  if ! cleanup_get_migration_status "$migration_id" response; then
    return 1
  fi

  if ! migration_status="$(
    jq -er '.migration.status' \
      <<<"$response" 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to read migration status for migration $migration_id during cleanup."

    return 1
  fi

  if ! combined_status="$(
    jq -r '.combined_state.status // empty' \
      <<<"$response" 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to read combined status for migration $migration_id during cleanup."

    return 1
  fi

  cleanup_log \
    "Migration $migration_id cleanup status: migration=$migration_status combined=${combined_status:-<none>}"

  if cleanup_status_is_terminal "$migration_status"; then
    return 0
  fi

  if cleanup_status_is_terminal "$combined_status"; then
    return 0
  fi

  return 1
}

cleanup_revert_cutover() {
  local migration_id="$1"
  local response
  local artifact_id
  local evidence_file
  local invalid_evidence_file
  local temporary_file
  local source_unarchived
  local cutover_terminated
  local migration_terminated

  if ! migration_is_owned "$migration_id"; then
    cleanup_log \
      "Refusing to revert cutover for unowned migration $migration_id."

    return 1
  fi

  if ! migration_started_cutover "$migration_id"; then
    cleanup_log \
      "Refusing to revert cutover for migration $migration_id because this run did not record a cutover attempt."

    return 1
  fi

  if migration_cleanup_is_complete "$migration_id"; then
    cleanup_log \
      "Refusing to revert cutover for migration $migration_id because cleanup is already complete."

    return 1
  fi

  artifact_id="$(migration_artifact_id "$migration_id")"
  evidence_file="$OUTDIR/cleanup-revert-cutover-${artifact_id}.json"
  invalid_evidence_file="$OUTDIR/cleanup-revert-cutover-invalid-${artifact_id}.json"
  temporary_file="${evidence_file}.tmp"

  cleanup_log \
    "Attempting cutover revert for run-owned migration $migration_id."

  # revert-cutover is the canonical command in older candidates and a retained
  # compatibility alias in newer candidates. Use the flag form because older
  # candidates do not accept the migration ID positionally.
  if ! response="$(
    gh elm migration revert-cutover \
      --migration-id "$migration_id" \
      --json 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Cutover revert command failed for migration $migration_id."

    return 1
  fi

  # A successful cleanup must confirm that the source repository was
  # unarchived. Terminal migration status alone is not proof that cutover was
  # safely reversed.
  if ! jq -e '
    (type == "object") and
    (.success | type == "boolean") and
    .success == true and
    (.unarchived_source_repository | type == "boolean") and
    (
      (has("in_progress_cutover_terminated") | not) or
      (.in_progress_cutover_terminated | type == "boolean")
    ) and
    (
      (has("in_progress_migration_terminated") | not) or
      (.in_progress_migration_terminated | type == "boolean")
    )
  ' >/dev/null <<<"$response" 2>>"$CLEANUP_LOG"; then
    cleanup_log \
      "Cutover revert returned an invalid or unsuccessful response for migration $migration_id."

    if ! printf '%s\n' "$response" >"$invalid_evidence_file"; then
      cleanup_log \
        "Failed to save the invalid cleanup revert response for migration $migration_id."
    fi

    return 1
  fi

  # A false value means the source repository was already unarchived. Operation
  # success is determined by the validated success field above.
  if ! source_unarchived="$(
    jq -r '.unarchived_source_repository' \
      <<<"$response" 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to read source restoration state for migration $migration_id."

    return 1
  fi

  # These optional fields default to false, matching the typed CLI response.
  # The validation above rejects any present non-boolean value.
  if ! cutover_terminated="$(
    jq -r '.in_progress_cutover_terminated // false' \
      <<<"$response" 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to read cutover termination state for migration $migration_id."

    return 1
  fi

  if ! migration_terminated="$(
    jq -r '.in_progress_migration_terminated // false' \
      <<<"$response" 2>>"$CLEANUP_LOG"
  )"; then
    cleanup_log \
      "Failed to read migration termination state for migration $migration_id."

    return 1
  fi

  # The remote revert has succeeded and explicitly confirmed that the source
  # repository was restored. Mark cleanup complete before writing evidence so
  # an evidence failure cannot cause a second revert attempt.
  if ! mark_cleanup_complete "$migration_id"; then
    cleanup_log \
      "Cutover was reverted, but migration $migration_id could not be marked cleanup-complete."

    return 1
  fi

  if ! printf '%s\n' "$response" >"$temporary_file"; then
    rm -f "$temporary_file"

    cleanup_log \
      "Cutover was reverted, but cleanup evidence could not be saved for migration $migration_id."

    return 1
  fi

  if ! mv "$temporary_file" "$evidence_file"; then
    rm -f "$temporary_file"

    cleanup_log \
      "Cutover was reverted, but cleanup evidence could not be finalized for migration $migration_id."

    return 1
  fi

  cleanup_log \
    "Cutover reverted successfully for migration $migration_id: source_unarchived=$source_unarchived, cutover_terminated=$cutover_terminated, migration_terminated=$migration_terminated."

  return 0
}

cleanup_cancel_migration() {
  local migration_id="$1"

  if ! migration_is_owned "$migration_id"; then
    cleanup_log \
      "Refusing to cancel unowned migration $migration_id."

    return 1
  fi

  if migration_started_cutover "$migration_id"; then
    cleanup_log \
      "Refusing to use cancellation as cleanup for migration $migration_id because this run recorded a cutover attempt."

    return 1
  fi

  if migration_cleanup_is_complete "$migration_id"; then
    cleanup_log \
      "Refusing to cancel migration $migration_id because cleanup is already complete."

    return 1
  fi

  cleanup_log \
    "Attempting cancellation of run-owned migration $migration_id."

  # Use the flag form because older candidates do not accept the migration ID
  # positionally.
  if ! gh elm migration cancel \
    --migration-id "$migration_id" \
    >>"$CLEANUP_LOG" 2>&1; then
    cleanup_log \
      "Cancellation command failed for migration $migration_id."

    return 1
  fi

  if ! mark_cancelled "$migration_id"; then
    cleanup_log \
      "Migration $migration_id was cancelled, but it could not be marked cleanup-complete."

    return 1
  fi

  cleanup_log \
    "Cancelled migration $migration_id successfully."

  return 0
}

cleanup_one_migration() {
  local migration_id="$1"

  if ! migration_is_owned "$migration_id"; then
    cleanup_log \
      "Refusing to clean up unowned migration ID $migration_id."

    return 1
  fi

  if migration_cleanup_is_complete "$migration_id"; then
    if migration_was_cancelled "$migration_id"; then
      cleanup_log \
        "Skipping migration $migration_id: already cancelled."
    elif migration_started_cutover "$migration_id"; then
      cleanup_log \
        "Skipping migration $migration_id: cutover cleanup already complete."
    else
      cleanup_log \
        "Skipping migration $migration_id: cleanup already complete."
    fi

    return 0
  fi

  # initiate_cutover records this state before sending its API request. This
  # closes the interruption window where the server accepts cutover but the
  # client exits before observing success.
  #
  # Once cutover may have started, cancellation or terminal migration status
  # cannot prove that the source repository was restored. Cleanup must therefore
  # require a successful revert that explicitly confirms the source was
  # unarchived.
  if migration_started_cutover "$migration_id"; then
    if cleanup_revert_cutover "$migration_id"; then
      return 0
    fi

    cleanup_log \
      "Cutover cleanup did not complete successfully for migration $migration_id. Refusing to downgrade this failure to cancellation or terminal-state cleanup."

    return 1
  fi

  if cleanup_cancel_migration "$migration_id"; then
    return 0
  fi

  # Cancellation can be rejected when a migration that never entered cutover
  # has already settled into a terminal state. Only non-cutover migrations may
  # use terminal status as evidence that no further cleanup is required.
  if cleanup_migration_is_terminal "$migration_id"; then
    if ! mark_cleanup_complete "$migration_id"; then
      cleanup_log \
        "Migration $migration_id is terminal, but it could not be marked cleanup-complete."

      return 1
    fi

    cleanup_log \
      "Non-cutover migration $migration_id is already terminal; no further cleanup is required."

    return 0
  fi

  cleanup_log \
    "Failed to cancel or confirm a terminal state for non-cutover migration $migration_id."

  return 1
}

record_cleanup_result() {
  local cleanup_failed="$1"

  if ((cleanup_failed != 0)); then
    if ! record_result \
      "Cleanup" \
      "❌ fail" \
      "One or more run-owned migrations could not be safely reverted, cancelled, or confirmed terminal before cutover. See cleanup.log."; then
      cleanup_log \
        "Failed to record the cleanup failure result."
    fi

    return 1
  fi

  if ! record_result \
    "Cleanup" \
    "✅ pass" \
    "All run-owned migrations were safely reverted, cancelled, or confirmed terminal before cutover."; then
    cleanup_log \
      "Failed to record the successful cleanup result."

    return 1
  fi

  return 0
}

record_overall_result() {
  local final_status="$1"

  if ((final_status == 0)); then
    if ! record_result \
      "Overall result" \
      "✅ pass" \
      "The GHES $E2E_MODE E2E scenario completed successfully."; then
      cleanup_log \
        "Failed to record the successful overall result."

      return 1
    fi

    return 0
  fi

  if ! record_result \
    "Overall result" \
    "❌ fail" \
    "The GHES $E2E_MODE E2E scenario failed. Run-scoped cleanup was attempted."; then
    cleanup_log \
      "Failed to record the failed overall result."

    return 1
  fi

  return 0
}

cleanup() {
  local original_status=$?
  local final_status="$original_status"
  local cleanup_failed=0
  local overall_result_failed=0
  local migration_id
  local migration_count=0

  # Prevent recursive cleanup if a command below exits unexpectedly.
  trap - EXIT INT TERM

  # Cleanup must continue through all run-owned migrations even when an
  # individual operation fails.
  set +e

  if declare -F owned_migration_count >/dev/null 2>&1; then
    migration_count="$(owned_migration_count)"
  fi

  cleanup_log \
    "Cleanup started for $migration_count run-owned migration(s)."

  for migration_id in "${CREATED_MIGRATION_IDS[@]}"; do
    if ! cleanup_one_migration "$migration_id"; then
      cleanup_failed=1
    fi
  done

  if ! record_cleanup_result "$cleanup_failed"; then
    cleanup_failed=1
  fi

  if ((cleanup_failed != 0 && final_status == 0)); then
    final_status=1
  fi

  if ! record_overall_result "$final_status"; then
    overall_result_failed=1
    final_status=1
  fi

  # If recording the overall result failed, make a best-effort attempt to leave
  # a failed result in the evidence after setting the final status to nonzero.
  if ((overall_result_failed != 0)); then
    record_result \
      "Overall result" \
      "❌ fail" \
      "The GHES $E2E_MODE E2E scenario could not finalize its evidence. See cleanup.log." \
      >/dev/null 2>&1 || true
  fi

  cleanup_log \
    "Cleanup finished with original status $original_status and final status $final_status."

  exit "$final_status"
}
