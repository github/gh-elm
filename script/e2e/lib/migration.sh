#!/usr/bin/env bash
#
# Source-side migration operations for the gh-elm control-plane E2E harness.
#
# This module provides:
#
#   - read-only migration-list preflight;
#   - migration creation and status validation;
#   - migration-list pagination validation;
#   - explicit migration cancellation.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

migration_command_log_section() {
  local title="$1"

  if [[ -z "${COMMAND_LOG:-}" ]]; then
    fail \
      "Harness" \
      "Command logging is unavailable; initialize_harness must run first."
  fi

  {
    printf '\n'
    printf '=== %s ===\n' "$title"
  } >>"$COMMAND_LOG"
}

migration_artifact_id() {
  local migration_id="$1"

  # Migration IDs are expected to be UUIDs, but sanitize the value before using
  # it in an artifact path so an unexpected API value cannot escape OUTDIR.
  printf '%s' "$migration_id" |
    tr -c 'A-Za-z0-9._-' '_'
}

run_preflight() {
  local output
  local page_count
  local total_count

  log "Running the read-only migration-list preflight."
  migration_command_log_section "Migration list preflight"

  if ! output="$(
    gh elm migration list \
      --status all \
      --page-size 1 \
      --json 2>>"$COMMAND_LOG"
  )"; then
    fail \
      "Preflight" \
      "The migration list request failed. Check source connectivity and credentials; see commands.log."
  fi

  if ! jq -e '
    (.migrations | type == "array") and
    (.total_count | type == "number") and
    (
      .next_cursor == null or
      (.next_cursor | type == "string")
    )
  ' >/dev/null <<<"$output"; then
    printf '%s\n' "$output" \
      >"$OUTDIR/preflight-invalid-response.json"

    fail \
      "Preflight" \
      "The migration list endpoint returned unexpected JSON."
  fi

  if ! printf '%s\n' "$output" >"$OUTDIR/preflight.json"; then
    fail \
      "Preflight" \
      "Failed to save migration-list preflight evidence."
  fi

  page_count="$(
    jq -r '.migrations | length' <<<"$output"
  )"

  total_count="$(
    jq -r '.total_count' <<<"$output"
  )"

  record_result \
    "Preflight" \
    "✅ pass" \
    "List returned valid JSON ($page_count on this page, $total_count total)."
}

create_migration() {
  local target_repo="$1"
  local result_key="${2:-Create migration}"
  local output
  local migration_id
  local evidence_file
  local temporary_file

  if [[ -z "$target_repo" ]]; then
    fail \
      "$result_key" \
      "Refusing to create a migration with an empty target repository name."
  fi

  if [[ "$target_repo" == */* ||
    "$target_repo" == *$'\n'* ||
    "$target_repo" == *$'\r'* ||
    "$target_repo" =~ [[:cntrl:]] ]]; then
    fail \
      "$result_key" \
      "The target repository name is invalid."
  fi

  log "Creating migration for $TARGET_ORG/$target_repo."

  migration_command_log_section \
    "Create migration: $SOURCE_ORG/$SOURCE_REPO -> $TARGET_ORG/$target_repo"

  if ! output="$(
    gh elm migration create \
      "$SOURCE_ORG/$SOURCE_REPO" \
      "$TARGET_ORG/$target_repo" \
      --target-visibility "$TARGET_VISIBILITY" \
      --json 2>>"$COMMAND_LOG"
  )"; then
    fail \
      "$result_key" \
      "Failed to create a migration for $TARGET_ORG/$target_repo; see commands.log."
  fi

  if ! migration_id="$(
    jq -er '
      .migration_id |
      select(type == "string" and length > 0)
    ' <<<"$output"
  )"; then
    fail \
      "$result_key" \
      "Migration creation succeeded but returned no migration_id."
  fi

  # Persist ownership immediately after obtaining the ID. No additional
  # assertion may occur before this call because cleanup depends on the
  # ownership record.
  remember_migration "$migration_id"
  LAST_MIGRATION_ID="$migration_id"

  evidence_file="$OUTDIR/create-${target_repo}.json"
  temporary_file="${evidence_file}.tmp"

  # Save the creation response only after recording ownership. If evidence
  # storage fails, the EXIT trap can still clean up the created migration.
  if ! printf '%s\n' "$output" >"$temporary_file"; then
    rm -f "$temporary_file"

    fail \
      "$result_key" \
      "Migration creation succeeded, but its response evidence could not be saved."
  fi

  if ! mv "$temporary_file" "$evidence_file"; then
    rm -f "$temporary_file"

    fail \
      "$result_key" \
      "Migration creation succeeded, but its response evidence could not be finalized."
  fi

  record_result \
    "$result_key" \
    "✅ pass" \
    "Created migration $migration_id for $TARGET_ORG/$target_repo."
}

get_migration_status() {
  local migration_id="$1"
  local artifact_id
  local output
  local temporary_file
  local status_file

  require_owned_migration "$migration_id"

  artifact_id="$(migration_artifact_id "$migration_id")"
  status_file="$OUTDIR/status-${artifact_id}.json"
  temporary_file="${status_file}.tmp"

  if ! output="$(
    gh elm migration status \
      --migration-id "$migration_id" \
      --json 2>>"$COMMAND_LOG"
  )"; then
    return 1
  fi

  # Keep the latest status response as evidence. Write atomically so
  # interruption cannot leave truncated JSON.
  if ! printf '%s\n' "$output" >"$temporary_file"; then
    rm -f "$temporary_file"
    return 1
  fi

  if ! mv "$temporary_file" "$status_file"; then
    rm -f "$temporary_file"
    return 1
  fi

  printf '%s\n' "$output"
}

verify_status() {
  local migration_id="$1"
  local target_repo="$2"
  local result_key="${3:-Migration status}"
  local output
  local migration_status
  local combined_status

  require_owned_migration "$migration_id"

  log "Retrieving status for migration $migration_id."
  migration_command_log_section "Migration status: $migration_id"

  if ! output="$(get_migration_status "$migration_id")"; then
    fail \
      "$result_key" \
      "Failed to retrieve migration $migration_id; see commands.log."
  fi

  if ! jq -e \
    --arg migration_id "$migration_id" \
    --arg source_org "$SOURCE_ORG" \
    --arg source_repo "$SOURCE_REPO" \
    --arg target_org "$TARGET_ORG" \
    --arg target_repo "$target_repo" '
      (.migration | type == "object") and
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
      ) and
      (
        .migration.status |
        type == "string" and length > 0
      )
    ' >/dev/null <<<"$output"; then
    fail \
      "$result_key" \
      "The status response did not match the run-owned migration."
  fi

  migration_status="$(
    jq -r '.migration.status' <<<"$output"
  )"

  combined_status="$(
    jq -r '.combined_state.status // empty' <<<"$output"
  )"

  record_result \
    "$result_key" \
    "✅ pass" \
    "Migration $migration_id is in state $migration_status${combined_status:+ with combined state $combined_status}."
}

verify_pagination() {
  local primary_id="$1"
  local pagination_id="$2"
  local cursor=""
  local next_cursor
  local output
  local listed_id
  local page_number=0
  local transitions=0
  local found_primary=0
  local found_pagination=0
  local max_pages=200

  local -a list_args
  declare -A seen_cursors=()

  require_owned_migration "$primary_id"
  require_owned_migration "$pagination_id"

  if [[ "$primary_id" == "$pagination_id" ]]; then
    fail \
      "List pagination" \
      "Pagination verification requires two distinct migrations."
  fi

  log "Walking migration-list pages to find both run-owned migrations."

  migration_command_log_section \
    "Migration list pagination: $primary_id and $pagination_id"

  while ((page_number < max_pages)); do
    page_number=$((page_number + 1))

    list_args=(
      migration
      list
      --status
      created
      --page-size
      1
      --json
    )

    if [[ -n "$cursor" ]]; then
      list_args+=(--after "$cursor")
    fi

    if ! output="$(
      gh elm "${list_args[@]}" 2>>"$COMMAND_LOG"
    )"; then
      fail \
        "List pagination" \
        "Migration listing failed on page $page_number; see commands.log."
    fi

    if ! printf '%s\n' "$output" \
      >"$OUTDIR/list-page-${page_number}.json"; then
      fail \
        "List pagination" \
        "Failed to save migration-list evidence for page $page_number."
    fi

    if ! jq -e '
      (.migrations | type == "array") and
      (
        .next_cursor == null or
        (.next_cursor | type == "string")
      )
    ' >/dev/null <<<"$output"; then
      fail \
        "List pagination" \
        "Page $page_number returned unexpected JSON."
    fi

    while IFS= read -r listed_id; do
      [[ -n "$listed_id" ]] || continue

      if [[ "$listed_id" == "$primary_id" ]]; then
        found_primary=1
      fi

      if [[ "$listed_id" == "$pagination_id" ]]; then
        found_pagination=1
      fi
    done < <(
      jq -r '
        .migrations[]? |
        .migration_id // empty
      ' <<<"$output"
    )

    if ((found_primary == 1 && found_pagination == 1)); then
      break
    fi

    next_cursor="$(
      jq -r '.next_cursor // empty' <<<"$output"
    )"

    if [[ -z "$next_cursor" ]]; then
      break
    fi

    if [[ "$next_cursor" == "$cursor" ||
      -n "${seen_cursors[$next_cursor]:-}" ]]; then
      fail \
        "List pagination" \
        "The migration list API returned a repeated pagination cursor."
    fi

    cursor="$next_cursor"
    seen_cursors["$cursor"]=1
    transitions=$((transitions + 1))
  done

  if ((found_primary == 0 || found_pagination == 0)); then
    fail \
      "List pagination" \
      "Both run-owned migrations were not found within $page_number page(s)."
  fi

  if ((transitions == 0)); then
    fail \
      "List pagination" \
      "Both migrations were found, but no pagination cursor was followed."
  fi

  record_result \
    "List pagination" \
    "✅ pass" \
    "Found both migrations after $page_number page(s) and $transitions cursor transition(s)."
}

cancel_migration() {
  local migration_id="$1"
  local result_key="${2:-Cancel migration}"

  require_owned_migration "$migration_id"

  if migration_cleanup_is_complete "$migration_id"; then
    fail \
      "$result_key" \
      "Refusing to cancel migration $migration_id because its cleanup is already complete."
  fi

  log "Cancelling migration $migration_id."
  migration_command_log_section "Cancel migration: $migration_id"

  if ! gh elm migration cancel \
    --migration-id "$migration_id" \
    >>"$COMMAND_LOG" 2>&1; then
    fail \
      "$result_key" \
      "Failed to cancel run-owned migration $migration_id; see commands.log."
  fi

  mark_cancelled "$migration_id"

  record_result \
    "$result_key" \
    "✅ pass" \
    "Cancelled run-owned migration $migration_id."
}
