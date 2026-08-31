#!/usr/bin/env bash
#
# Polling and asynchronous state verification for the gh-elm E2E harness.
#
# This module provides:
#
#   - target migration ID polling;
#   - cutover-readiness polling;
#   - cutover-completion polling;
#   - post-revert state verification.
#
# Source-side commands and one-shot operations live in migration.sh.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

# Avoid failing a polling operation because of one transient status error.
# The counter is reset after every valid response.
POLLING_MAX_CONSECUTIVE_STATUS_FAILURES=3

polling_now() {
  date +%s
}

polling_timestamp() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}

polling_elapsed() {
  local started_at="$1"
  local now

  now="$(polling_now)"

  # Avoid a negative elapsed time if the system clock moves backward.
  if ((now < started_at)); then
    printf '0\n'
    return 0
  fi

  printf '%d\n' "$((now - started_at))"
}

polling_sleep() {
  sleep "$E2E_POLL_INTERVAL_SECONDS"
}

validate_polling_timeout() {
  local timeout_seconds="$1"
  local result_key="$2"
  local description="$3"

  if [[ ! "$timeout_seconds" =~ ^[0-9]+$ ]] ||
    ((10#$timeout_seconds <= 0)); then
    fail \
      "$result_key" \
      "$description must be a positive integer, not ${timeout_seconds:-<empty>}."
  fi
}

migration_status_from_response() {
  local response="$1"

  jq -r '.migration.status // empty' <<<"$response"
}

combined_status_from_response() {
  local response="$1"

  jq -r '.combined_state.status // empty' <<<"$response"
}

ready_for_cutover_from_response() {
  local response="$1"

  jq -r '
    if .combined_state.ready_for_cutover == true then
      "true"
    else
      "false"
    end
  ' <<<"$response"
}

migration_state_is_failure() {
  local status="$1"

  case "$status" in
    failed | terminated | cancelled | canceled | aborted | expired)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

lifecycle_status_is() {
  local migration_status="$1"
  local combined_status="$2"
  local expected_status="$3"

  # combined_state.status is authoritative whenever it is present. Fall back
  # to migration.status only when the combined status is unavailable.
  if [[ -n "$combined_status" ]]; then
    [[ "$combined_status" == "$expected_status" ]]
  else
    [[ "$migration_status" == "$expected_status" ]]
  fi
}

polling_artifact_purpose() {
  local purpose="$1"
  local safe_purpose

  safe_purpose="$(
    printf '%s' "$purpose" |
      tr '[:upper:]' '[:lower:]' |
      tr -c 'a-z0-9._-' '-' |
      sed -E \
        -e 's/^-+//' \
        -e 's/-+$//'
  )"

  if [[ -z "$safe_purpose" ]]; then
    safe_purpose="poll"
  fi

  printf '%s\n' "$safe_purpose"
}

record_poll_snapshot() {
  local migration_id="$1"
  local purpose="$2"
  local attempt="$3"
  local response="$4"
  local artifact_id
  local safe_purpose
  local first_file
  local first_temporary_file
  local latest_file
  local latest_temporary_file
  local timeline_file
  local timeline_entry
  local timestamp

  artifact_id="$(migration_artifact_id "$migration_id")"
  safe_purpose="$(polling_artifact_purpose "$purpose")"

  first_file="$OUTDIR/poll-${safe_purpose}-${artifact_id}-first.json"
  first_temporary_file="${first_file}.tmp"
  latest_file="$OUTDIR/poll-${safe_purpose}-${artifact_id}-latest.json"
  latest_temporary_file="${latest_file}.tmp"
  timeline_file="$OUTDIR/poll-timeline.ndjson"
  timestamp="$(polling_timestamp)"

  # Preserve the first state observed for each polling operation. Write it
  # atomically so interruption cannot leave a truncated first-state artifact.
  if [[ ! -e "$first_file" ]]; then
    if ! printf '%s\n' "$response" >"$first_temporary_file"; then
      rm -f "$first_temporary_file"
      return 1
    fi

    if ! mv "$first_temporary_file" "$first_file"; then
      rm -f "$first_temporary_file"
      return 1
    fi
  fi

  # Write the latest response atomically so termination cannot leave a
  # partially written JSON artifact.
  if ! printf '%s\n' "$response" >"$latest_temporary_file"; then
    rm -f "$latest_temporary_file"
    return 1
  fi

  if ! mv "$latest_temporary_file" "$latest_file"; then
    rm -f "$latest_temporary_file"
    return 1
  fi

  # Append a compact timeline entry containing only state information useful
  # for diagnosis. Full first/latest API responses remain available separately.
  if ! timeline_entry="$(
    jq -c \
      --arg observed_at "$timestamp" \
      --arg purpose "$purpose" \
      --argjson attempt "$attempt" '
        {
          observed_at: $observed_at,
          purpose: $purpose,
          attempt: $attempt,
          migration_id: (.migration.migration_id // null),
          migration_status: (.migration.status // null),
          combined_status: (.combined_state.status // null),
          ready_for_cutover:
            (.combined_state.ready_for_cutover // false),
          cutover_blockers:
            (.combined_state.cutover_blockers // [])
        }
      ' <<<"$response"
  )"; then
    return 1
  fi

  if ! printf '%s\n' "$timeline_entry" >>"$timeline_file"; then
    return 1
  fi
}

validate_status_response() {
  local response="$1"
  local expected_migration_id="${2:-}"

  jq -e \
    --arg expected_migration_id "$expected_migration_id" '
      (.migration | type == "object") and
      (
        .migration.migration_id |
        type == "string" and length > 0
      ) and
      (
        $expected_migration_id == "" or
        .migration.migration_id == $expected_migration_id
      ) and
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
      ) and
      (
        .combined_state == null or
        .combined_state.ready_for_cutover == null or
        (.combined_state.ready_for_cutover | type == "boolean")
      ) and
      (
        .combined_state == null or
        .combined_state.cutover_blockers == null or
        (.combined_state.cutover_blockers | type == "array")
      )
    ' >/dev/null <<<"$response"
}

read_poll_status() {
  local migration_id="$1"
  local purpose="$2"
  local attempt="$3"

  # Assign the fetched response to the variable named by the fourth argument.
  # The fetched value deliberately uses a different local name so the nameref
  # cannot resolve to a local variable in this function when callers pass a
  # variable named "response".
  local -n response_ref="$4"
  local fetched_response
  local artifact_id
  local safe_purpose
  local invalid_response_file

  if ! fetched_response="$(get_migration_status "$migration_id")"; then
    printf 'Status request failed for migration %s during %s attempt %s.\n' \
      "$migration_id" \
      "$purpose" \
      "$attempt" >>"$COMMAND_LOG"

    return 1
  fi

  artifact_id="$(migration_artifact_id "$migration_id")"
  safe_purpose="$(polling_artifact_purpose "$purpose")"

  if ! validate_status_response \
    "$fetched_response" \
    "$migration_id"; then
    printf 'Unexpected status response for migration %s during %s attempt %s.\n' \
      "$migration_id" \
      "$purpose" \
      "$attempt" >>"$COMMAND_LOG"

    invalid_response_file="$OUTDIR/poll-invalid-${safe_purpose}-${artifact_id}-${attempt}.json"

    if ! printf '%s\n' "$fetched_response" >"$invalid_response_file"; then
      printf 'Failed to save invalid status response for migration %s during %s attempt %s.\n' \
        "$migration_id" \
        "$purpose" \
        "$attempt" >>"$COMMAND_LOG"
    fi

    return 1
  fi

  if ! record_poll_snapshot \
    "$migration_id" \
    "$purpose" \
    "$attempt" \
    "$fetched_response"; then
    printf 'Failed to record polling evidence for migration %s during %s attempt %s.\n' \
      "$migration_id" \
      "$purpose" \
      "$attempt" >>"$COMMAND_LOG"

    return 1
  fi

  response_ref="$fetched_response"
}

polling_handle_status_failure() {
  local result_key="$1"
  local migration_id="$2"
  local purpose="$3"
  local failure_count="$4"

  log \
    "Migration $migration_id $purpose status request failed ($failure_count/$POLLING_MAX_CONSECUTIVE_STATUS_FAILURES consecutive failures)."

  if ((failure_count >= POLLING_MAX_CONSECUTIVE_STATUS_FAILURES)); then
    fail \
      "$result_key" \
      "Failed to retrieve a valid status for migration $migration_id during $purpose after $failure_count consecutive attempts."
  fi
}

fail_on_terminal_migration_state() {
  local result_key="$1"
  local migration_id="$2"
  local expected_description="$3"
  local migration_status="$4"
  local combined_status="$5"

  # combined_state.status is authoritative whenever it is present. Do not
  # diagnose a fallback migration.status failure if combined state reports a
  # different, current lifecycle state.
  if [[ -n "$combined_status" ]]; then
    if migration_state_is_failure "$combined_status"; then
      fail \
        "$result_key" \
        "Migration $migration_id entered terminal combined state $combined_status while waiting for $expected_description."
    fi

    return 0
  fi

  if migration_state_is_failure "$migration_status"; then
    fail \
      "$result_key" \
      "Migration $migration_id entered terminal state $migration_status while waiting for $expected_description."
  fi
}

resolve_target_migration_id() {
  local migration_id="$1"
  local result_key="${2:-Target migration ID}"
  local started_at
  local elapsed
  local attempt=0
  local response
  local status_response
  local target_migration_id
  local migration_status
  local combined_status
  local artifact_id
  local evidence_file
  local temporary_file
  local consecutive_status_failures=0
  local lookup_succeeded

  require_owned_migration "$migration_id"

  artifact_id="$(migration_artifact_id "$migration_id")"
  evidence_file="$OUTDIR/target-id-${artifact_id}.json"
  temporary_file="${evidence_file}.tmp"
  started_at="$(polling_now)"

  # Callers use command substitution to capture the numeric ID. Keep stdout
  # reserved for that value and send progress/result logging to stderr.
  log "Resolving the target migration ID for $migration_id." >&2

  migration_command_log_section \
    "Resolve target migration ID: $migration_id"

  while true; do
    attempt=$((attempt + 1))
    lookup_succeeded=0

    # lookup-target-id is the canonical command in older candidates and a
    # retained compatibility alias in newer candidates.
    if response="$(
      gh elm migration lookup-target-id \
        --migration-id "$migration_id" \
        --json 2>>"$COMMAND_LOG"
    )"; then
      lookup_succeeded=1

      if target_migration_id="$(
        jq -er \
          --arg migration_id "$migration_id" '
            select(.migration_id == $migration_id) |
            .target_migration_id |
            select(type == "number" and . > 0)
          ' <<<"$response"
      )"; then
        # Save only a validated successful lookup response. Write atomically so
        # interruption cannot leave a partial target-ID artifact.
        if ! printf '%s\n' "$response" >"$temporary_file"; then
          rm -f "$temporary_file"

          fail \
            "$result_key" \
            "Failed to save target migration ID evidence for migration $migration_id." >&2
        fi

        if ! mv "$temporary_file" "$evidence_file"; then
          rm -f "$temporary_file"

          fail \
            "$result_key" \
            "Failed to finalize target migration ID evidence for migration $migration_id." >&2
        fi

        record_result \
          "$result_key" \
          "✅ pass" \
          "Resolved target migration ID $target_migration_id for migration $migration_id." >&2

        printf '%s\n' "$target_migration_id"
        return 0
      fi
    fi

    # If the target ID is not available yet, inspect source-side state so
    # terminal failures are reported promptly.
    if read_poll_status \
      "$migration_id" \
      "target-id" \
      "$attempt" \
      status_response; then
      consecutive_status_failures=0

      migration_status="$(
        migration_status_from_response "$status_response"
      )"

      combined_status="$(
        combined_status_from_response "$status_response"
      )"

      log \
        "Migration $migration_id target-ID wait: migration=$migration_status combined=$combined_status" >&2

      fail_on_terminal_migration_state \
        "$result_key" \
        "$migration_id" \
        "a target migration ID" \
        "$migration_status" \
        "$combined_status"

      # A completed migration proves that no ID will appear only when the
      # lookup request itself succeeded. A transient lookup command failure
      # must remain recoverable and continue polling.
      if ((lookup_succeeded == 1)) &&
        lifecycle_status_is \
          "$migration_status" \
          "$combined_status" \
          "completed"; then
        fail \
          "$result_key" \
          "Migration $migration_id completed without exposing a positive target migration ID." >&2
      fi
    else
      consecutive_status_failures=$((consecutive_status_failures + 1))

      polling_handle_status_failure \
        "$result_key" \
        "$migration_id" \
        "target-ID resolution" \
        "$consecutive_status_failures" >&2
    fi

    elapsed="$(polling_elapsed "$started_at")"

    if ((elapsed >= E2E_STATE_TIMEOUT_SECONDS)); then
      fail \
        "$result_key" \
        "Timed out after ${E2E_STATE_TIMEOUT_SECONDS}s waiting for migration $migration_id to expose a target migration ID. Last states: migration=${migration_status:-unknown}, combined=${combined_status:-unknown}." >&2
    fi

    polling_sleep
  done
}

wait_for_cutover_readiness() {
  local migration_id="$1"
  local timeout_seconds="$2"
  local result_key="${3:-Wait for cutover readiness}"
  local started_at
  local elapsed
  local attempt=0
  local response
  local migration_status
  local combined_status
  local ready_for_cutover
  local blockers
  local consecutive_status_failures=0

  require_owned_migration "$migration_id"

  validate_polling_timeout \
    "$timeout_seconds" \
    "$result_key" \
    "Cutover-readiness timeout"

  started_at="$(polling_now)"

  log \
    "Waiting up to ${timeout_seconds}s for migration $migration_id to become ready for cutover."

  migration_command_log_section \
    "Wait for cutover readiness: $migration_id"

  while true; do
    attempt=$((attempt + 1))

    if ! read_poll_status \
      "$migration_id" \
      "cutover-readiness" \
      "$attempt" \
      response; then
      consecutive_status_failures=$((consecutive_status_failures + 1))

      polling_handle_status_failure \
        "$result_key" \
        "$migration_id" \
        "cutover-readiness polling" \
        "$consecutive_status_failures"
    else
      consecutive_status_failures=0

      migration_status="$(
        migration_status_from_response "$response"
      )"

      combined_status="$(
        combined_status_from_response "$response"
      )"

      ready_for_cutover="$(
        ready_for_cutover_from_response "$response"
      )"

      blockers="$(
        jq -r '
          [
            .combined_state.cutover_blockers[]?
            | select(type == "string" and length > 0)
          ] |
          if length == 0 then
            "none"
          else
            join("; ")
          end
        ' <<<"$response"
      )"

      log \
        "Migration $migration_id cutover-readiness wait: ready=$ready_for_cutover migration=$migration_status combined=$combined_status blockers=$blockers"

      # Evaluate authoritative terminal and completed states before accepting
      # readiness. This prevents a stale ready_for_cutover value from masking a
      # newer failed, terminated, or completed lifecycle state.
      fail_on_terminal_migration_state \
        "$result_key" \
        "$migration_id" \
        "cutover readiness" \
        "$migration_status" \
        "$combined_status"

      if lifecycle_status_is \
        "$migration_status" \
        "$combined_status" \
        "completed"; then
        fail \
          "$result_key" \
          "Migration $migration_id completed before the harness observed cutover readiness."
      fi

      if [[ "$ready_for_cutover" == "true" ||
        "$combined_status" == "ready_for_cutover" ]]; then
        record_result \
          "$result_key" \
          "✅ pass" \
          "Migration $migration_id became ready for cutover after $attempt status request(s)."

        return 0
      fi
    fi

    elapsed="$(polling_elapsed "$started_at")"

    if ((elapsed >= timeout_seconds)); then
      fail \
        "$result_key" \
        "Timed out after ${timeout_seconds}s waiting for cutover readiness. Last states: migration=${migration_status:-unknown}, combined=${combined_status:-unknown}. Blockers: ${blockers:-unknown}."
    fi

    polling_sleep
  done
}

wait_for_cutover_completion() {
  local migration_id="$1"
  local timeout_seconds="$2"
  local result_key="${3:-Wait for cutover completion}"
  local started_at
  local elapsed
  local attempt=0
  local response
  local migration_status
  local combined_status
  local consecutive_status_failures=0

  require_owned_migration "$migration_id"

  if ! migration_started_cutover "$migration_id"; then
    fail \
      "$result_key" \
      "Refusing to wait for cutover completion because this scenario did not initiate cutover for migration $migration_id."
  fi

  validate_polling_timeout \
    "$timeout_seconds" \
    "$result_key" \
    "Cutover-completion timeout"

  started_at="$(polling_now)"

  log \
    "Waiting up to ${timeout_seconds}s for migration $migration_id to complete cutover."

  migration_command_log_section \
    "Wait for cutover completion: $migration_id"

  while true; do
    attempt=$((attempt + 1))

    if ! read_poll_status \
      "$migration_id" \
      "cutover-completion" \
      "$attempt" \
      response; then
      consecutive_status_failures=$((consecutive_status_failures + 1))

      polling_handle_status_failure \
        "$result_key" \
        "$migration_id" \
        "cutover-completion polling" \
        "$consecutive_status_failures"
    else
      consecutive_status_failures=0

      migration_status="$(
        migration_status_from_response "$response"
      )"

      combined_status="$(
        combined_status_from_response "$response"
      )"

      log \
        "Migration $migration_id cutover-completion wait: migration=$migration_status combined=$combined_status"

      # combined_state.status is authoritative when present. A completed
      # migration.status must not override a combined cutting_over or failed
      # state.
      if lifecycle_status_is \
        "$migration_status" \
        "$combined_status" \
        "completed"; then
        record_result \
          "$result_key" \
          "✅ pass" \
          "Migration $migration_id completed cutover after $attempt status request(s)."

        return 0
      fi

      fail_on_terminal_migration_state \
        "$result_key" \
        "$migration_id" \
        "successful cutover completion" \
        "$migration_status" \
        "$combined_status"
    fi

    elapsed="$(polling_elapsed "$started_at")"

    if ((elapsed >= timeout_seconds)); then
      fail \
        "$result_key" \
        "Timed out after ${timeout_seconds}s waiting for cutover completion. Last states: migration=${migration_status:-unknown}, combined=${combined_status:-unknown}."
    fi

    polling_sleep
  done
}

verify_reverted_state() {
  local migration_id="$1"
  local target_repo="$2"
  local result_key="${3:-Verify reverted state}"
  local started_at
  local elapsed
  local attempt=0
  local response
  local migration_status
  local combined_status
  local consecutive_status_failures=0

  require_owned_migration "$migration_id"

  if ! migration_cleanup_is_complete "$migration_id"; then
    fail \
      "$result_key" \
      "Refusing to verify post-revert state before migration $migration_id has been marked cleanup-complete."
  fi

  started_at="$(polling_now)"

  log "Verifying the post-revert state for migration $migration_id."

  migration_command_log_section \
    "Verify post-revert state: $migration_id"

  while true; do
    attempt=$((attempt + 1))

    if ! read_poll_status \
      "$migration_id" \
      "post-revert" \
      "$attempt" \
      response; then
      consecutive_status_failures=$((consecutive_status_failures + 1))

      polling_handle_status_failure \
        "$result_key" \
        "$migration_id" \
        "post-revert status verification" \
        "$consecutive_status_failures"
    else
      consecutive_status_failures=0

      if ! jq -e \
        --arg migration_id "$migration_id" \
        --arg source_org "$SOURCE_ORG" \
        --arg source_repo "$SOURCE_REPO" \
        --arg target_org "$TARGET_ORG" \
        --arg target_repo "$target_repo" '
          .migration.migration_id == $migration_id and
          (
            .migration.source_organization_login |
            type == "string" and
            ascii_downcase == ($source_org | ascii_downcase)
          ) and
          (
            .migration.source_repository_name |
            type == "string" and
            ascii_downcase == ($source_repo | ascii_downcase)
          ) and
          (
            .migration.target_organization_login |
            type == "string" and
            ascii_downcase == ($target_org | ascii_downcase)
          ) and
          (
            .migration.target_repository_name |
            type == "string" and
            ascii_downcase == ($target_repo | ascii_downcase)
          )
        ' >/dev/null <<<"$response"; then
        fail \
          "$result_key" \
          "The post-revert status response did not match the run-owned migration."
      fi

      migration_status="$(
        migration_status_from_response "$response"
      )"

      combined_status="$(
        combined_status_from_response "$response"
      )"

      log \
        "Migration $migration_id post-revert wait: migration=$migration_status combined=$combined_status"

      # combined_state.status is authoritative whenever present. Only inspect
      # migration.status when the combined status is unavailable.
      if [[ -n "$combined_status" ]]; then
        case "$combined_status" in
          terminated | completed | cancelled | canceled)
            record_result \
              "$result_key" \
              "✅ pass" \
              "Migration $migration_id has accessible post-revert combined status $combined_status after $attempt status request(s)."

            return 0
            ;;
          failed | aborted | expired)
            fail \
              "$result_key" \
              "Migration $migration_id entered unexpected post-revert combined state $combined_status."
            ;;
        esac
      else
        case "$migration_status" in
          terminated | completed | cancelled | canceled)
            record_result \
              "$result_key" \
              "✅ pass" \
              "Migration $migration_id has accessible post-revert status $migration_status after $attempt status request(s)."

            return 0
            ;;
          failed | aborted | expired)
            fail \
              "$result_key" \
              "Migration $migration_id entered unexpected post-revert state $migration_status."
            ;;
        esac
      fi
    fi

    elapsed="$(polling_elapsed "$started_at")"

    if ((elapsed >= E2E_STATE_TIMEOUT_SECONDS)); then
      fail \
        "$result_key" \
        "Timed out after ${E2E_STATE_TIMEOUT_SECONDS}s waiting for an accessible terminal post-revert status. Last states: migration=${migration_status:-unknown}, combined=${combined_status:-unknown}."
    fi

    polling_sleep
  done
}
