#!/usr/bin/env bash
#
# Target-side migration operations for the gh-elm E2E harness.
#
# This module validates target migration IDs and exercises APIs exposed by:
#
#   gh elm target ...
#
# Target migration ID polling remains in polling.sh because it reads the
# source-side migration until the destination ID becomes available.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

validate_target_migration_id_value() {
  local target_migration_id="$1"
  local normalized_id

  if [[ -z "$target_migration_id" ]]; then
    return 1
  fi

  if [[ ! "$target_migration_id" =~ ^[0-9]+$ ]]; then
    return 1
  fi

  # Strip leading zeroes before arithmetic evaluation so Bash does not
  # interpret the value as octal.
  normalized_id="$(
    printf '%s' "$target_migration_id" |
      sed -E 's/^0+//'
  )"

  if [[ -z "$normalized_id" ]]; then
    normalized_id="0"
  fi

  ((10#$normalized_id > 0))
}

verify_target_migration_id() {
  local target_migration_id="$1"
  local result_key="${2:-Target migration ID validation}"

  if ! validate_target_migration_id_value "$target_migration_id"; then
    fail \
      "$result_key" \
      "Target migration ID must be a positive integer, not ${target_migration_id:-<empty>}."
  fi

  record_result \
    "$result_key" \
    "✅ pass" \
    "Target migration ID $target_migration_id is a positive integer."
}

validate_repository_nwo() {
  local repository_nwo="$1"
  local owner
  local repository

  if [[ -z "$repository_nwo" ]]; then
    return 1
  fi

  if [[ "$repository_nwo" == *$'\n'* ||
    "$repository_nwo" == *$'\r'* ||
    "$repository_nwo" =~ [[:space:]] ||
    "$repository_nwo" =~ [[:cntrl:]] ]]; then
    return 1
  fi

  if [[ "$repository_nwo" == -* ]]; then
    return 1
  fi

  if [[ "$repository_nwo" != */* ]]; then
    return 1
  fi

  owner="${repository_nwo%%/*}"
  repository="${repository_nwo#*/}"

  # Reject owner/repository/extra and empty owner or repository components.
  if [[ -z "$owner" ||
    -z "$repository" ||
    "$repository" == */* ]]; then
    return 1
  fi

  if [[ "$owner" == "." ||
    "$owner" == ".." ||
    "$repository" == "." ||
    "$repository" == ".." ]]; then
    return 1
  fi

  return 0
}

target_artifact_name() {
  local value="$1"
  local sanitized

  sanitized="$(
    printf '%s' "$value" |
      tr '[:upper:]' '[:lower:]' |
      tr -c 'a-z0-9._-' '-' |
      sed -E \
        -e 's/^-+//' \
        -e 's/-+$//'
  )"

  if [[ -z "$sanitized" ]]; then
    sanitized="target"
  fi

  printf '%s\n' "$sanitized"
}

verify_target_resources() {
  local target_migration_id="$1"
  local repository_nwo="$2"
  local result_key="${3:-Target resources}"
  local max_results=20
  local output
  local resource_count
  local artifact_repository
  local artifact_file
  local artifact_temporary_file
  local parsed_file
  local parsed_temporary_file

  if ! validate_target_migration_id_value "$target_migration_id"; then
    fail \
      "$result_key" \
      "Cannot list resources with invalid target migration ID ${target_migration_id:-<empty>}."
  fi

  if ! validate_repository_nwo "$repository_nwo"; then
    fail \
      "$result_key" \
      "Repository must be in owner/repository format, not ${repository_nwo:-<empty>}."
  fi

  if [[ -z "${OUTDIR:-}" || -z "${COMMAND_LOG:-}" ]]; then
    fail \
      "Harness" \
      "Evidence paths are unavailable; initialize_harness must run before target API checks."
  fi

  artifact_repository="$(
    target_artifact_name "$repository_nwo"
  )"

  artifact_file="$OUTDIR/target-resources-${target_migration_id}-${artifact_repository}.ndjson"
  artifact_temporary_file="${artifact_file}.tmp"

  parsed_file="$OUTDIR/target-resources-${target_migration_id}-${artifact_repository}.json"
  parsed_temporary_file="${parsed_file}.tmp"

  log \
    "Listing target resources for target migration $target_migration_id and repository $repository_nwo."

  migration_command_log_section \
    "Target resources: migration=$target_migration_id repository=$repository_nwo"

  # Use flags for compatibility with candidates that do not accept the target
  # migration ID or repository as positional operands. Newer candidates retain
  # these flags for backward compatibility.
  if ! output="$(
    gh elm target resources \
      --migration-id "$target_migration_id" \
      --repository "$repository_nwo" \
      --max-results "$max_results" \
      --json 2>>"$COMMAND_LOG"
  )"; then
    fail \
      "$result_key" \
      "Failed to list target resources for migration $target_migration_id and repository $repository_nwo; see commands.log."
  fi

  # Preserve the command's exact NDJSON output. An empty response is valid and
  # represents a migration for which no matching resource records were found.
  if [[ -n "$output" ]]; then
    if ! printf '%s\n' "$output" >"$artifact_temporary_file"; then
      rm -f "$artifact_temporary_file"

      fail \
        "$result_key" \
        "Failed to save target resource evidence."
    fi
  else
    if ! : >"$artifact_temporary_file"; then
      rm -f "$artifact_temporary_file"

      fail \
        "$result_key" \
        "Failed to save empty target resource evidence."
    fi
  fi

  if ! mv "$artifact_temporary_file" "$artifact_file"; then
    rm -f "$artifact_temporary_file"

    fail \
      "$result_key" \
      "Failed to finalize target resource evidence."
  fi

  # Slurp the NDJSON into an array, validate that every element is an object,
  # and emit the array itself. An empty input produces an empty array.
  if ! jq -s -e '
    if type == "array" and
      all(.[];
        type == "object"
      )
    then
      .
    else
      error("target resources output is not valid object NDJSON")
    end
  ' "$artifact_file" >"$parsed_temporary_file"; then
    rm -f "$parsed_temporary_file"

    fail \
      "$result_key" \
      "The target resources command returned invalid newline-delimited JSON."
  fi

  if ! mv "$parsed_temporary_file" "$parsed_file"; then
    rm -f "$parsed_temporary_file"

    fail \
      "$result_key" \
      "Failed to finalize parsed target resource evidence."
  fi

  if ! resource_count="$(
    jq -er '
      if type == "array" then
        length
      else
        error("parsed target resources are not an array")
      end
    ' "$parsed_file"
  )"; then
    fail \
      "$result_key" \
      "Failed to count target resource records."
  fi

  record_result \
    "$result_key" \
    "✅ pass" \
    "Target resources returned valid JSON with $resource_count resource record(s), capped at $max_results."
}
