#!/usr/bin/env bash
#
# Evidence initialization, structured metadata, and result recording for the
# gh-elm control-plane E2E harness.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

initialize_harness() {
  if [[ -z "${OUTDIR:-}" ]]; then
    printf 'OUTDIR must be set before initializing the E2E harness.\n' >&2
    return 1
  fi

  if ! mkdir -p "$OUTDIR"; then
    printf 'Failed to create E2E evidence directory: %s\n' "$OUTDIR" >&2
    return 1
  fi

  EVIDENCE_FILE="$OUTDIR/evidence.md"
  RESULTS_FILE="$OUTDIR/results.tsv"
  MIGRATIONS_FILE="$OUTDIR/created-migrations.txt"
  CLEANUP_LOG="$OUTDIR/cleanup.log"
  COMMAND_LOG="$OUTDIR/commands.log"
  METADATA_FILE="$OUTDIR/metadata.json"

  if ! : >"$EVIDENCE_FILE" ||
    ! : >"$RESULTS_FILE" ||
    ! : >"$MIGRATIONS_FILE" ||
    ! : >"$CLEANUP_LOG" ||
    ! : >"$COMMAND_LOG"; then
    printf 'Failed to initialize E2E evidence files under: %s\n' \
      "$OUTDIR" >&2
    return 1
  fi

  # metadata.json is written by write_evidence_header after configuration and
  # generated repository names are available. Remove stale content left by a
  # previous local invocation using the same OUTDIR.
  if ! rm -f "$METADATA_FILE"; then
    printf 'Failed to reset E2E metadata file: %s\n' \
      "$METADATA_FILE" >&2
    return 1
  fi
}

record_result() {
  local key="$1"
  local result="$2"
  local note="${3:-}"

  if [[ -z "${RESULTS_FILE:-}" || -z "${EVIDENCE_FILE:-}" ]]; then
    printf 'Cannot record result before initialize_harness has completed.\n' >&2
    return 1
  fi

  # Keep each result on one TSV line. This also prevents arbitrary command or
  # API error text from breaking the Markdown evidence structure.
  key="${key//$'\t'/ }"
  key="${key//$'\r'/ }"
  key="${key//$'\n'/ }"

  result="${result//$'\t'/ }"
  result="${result//$'\r'/ }"
  result="${result//$'\n'/ }"

  note="${note//$'\t'/ }"
  note="${note//$'\r'/ }"
  note="${note//$'\n'/ }"

  if ! printf '%s\t%s\t%s\n' \
    "$key" \
    "$result" \
    "$note" >>"$RESULTS_FILE"; then
    printf 'Failed to append result to: %s\n' "$RESULTS_FILE" >&2
    return 1
  fi

  if ! {
    printf '## %s\n\n' "$key"
    printf '**%s**\n' "$result"

    if [[ -n "$note" ]]; then
      printf '\n%s\n' "$note"
    fi

    printf '\n'
  } >>"$EVIDENCE_FILE"; then
    printf 'Failed to append evidence to: %s\n' "$EVIDENCE_FILE" >&2
    return 1
  fi

  log "$key: $result${note:+ — $note}"
}

write_evidence_header() {
  local target_repositories_json
  local temporary_metadata_file

  if [[ -z "${METADATA_FILE:-}" || -z "${EVIDENCE_FILE:-}" ]]; then
    fail \
      "Harness" \
      "Evidence paths are unavailable; initialize_harness must run first."
  fi

  if [[ -z "${TARGET_REPO_PRIMARY:-}" ||
    -z "${TARGET_REPO_PAGINATION:-}" ]]; then
    fail \
      "Harness" \
      "Runtime repository names are unavailable; configure_runtime must run before write_evidence_header."
  fi

  target_repositories_json="$(
    jq -cn \
      --arg primary "$TARGET_REPO_PRIMARY" \
      --arg pagination "$TARGET_REPO_PAGINATION" \
      '[$primary, $pagination]'
  )"

  temporary_metadata_file="${METADATA_FILE}.tmp"

  if ! jq -n \
    --arg run_id "$E2E_RUN_ID" \
    --arg scenario "$E2E_MODE" \
    --arg source_host "$SOURCE_HOST" \
    --arg source_repository "$SOURCE_ORG/$SOURCE_REPO" \
    --arg target_host "$TARGET_HOST" \
    --arg target_organization "$TARGET_ORG" \
    --arg target_visibility "$TARGET_VISIBILITY" \
    --argjson target_repositories "$target_repositories_json" \
    '{
      run_id: $run_id,
      scenario: $scenario,
      source_host: $source_host,
      source_repository: $source_repository,
      target_host: $target_host,
      target_organization: $target_organization,
      target_visibility: $target_visibility,
      target_repositories: $target_repositories
    }' >"$temporary_metadata_file"; then
    rm -f "$temporary_metadata_file"

    fail \
      "Evidence" \
      "Failed to write structured E2E metadata."
  fi

  if ! mv "$temporary_metadata_file" "$METADATA_FILE"; then
    rm -f "$temporary_metadata_file"

    fail \
      "Evidence" \
      "Failed to finalize structured E2E metadata."
  fi

  if ! {
    echo "# gh-elm GHES E2E evidence"
    echo
    echo "- Run ID: \`$E2E_RUN_ID\`"
    echo "- Scenario: \`$E2E_MODE\`"
    echo "- Source host: \`$SOURCE_HOST\`"
    echo "- Source fixture: \`$SOURCE_ORG/$SOURCE_REPO\`"
    echo "- Target host: \`$TARGET_HOST\`"
    echo "- Target organization: \`$TARGET_ORG\`"
    echo "- Target visibility: \`$TARGET_VISIBILITY\`"
    echo "- Primary target: \`$TARGET_ORG/$TARGET_REPO_PRIMARY\`"
    echo "- Pagination target: \`$TARGET_ORG/$TARGET_REPO_PAGINATION\`"
    echo
  } >>"$EVIDENCE_FILE"; then
    fail \
      "Evidence" \
      "Failed to write the E2E evidence header."
  fi
}
