#!/usr/bin/env bash
#
# Render gh-elm E2E scenario results as a compact Markdown report and create
# or update a sticky pull request comment.
#
# Scenario result artifacts are treated as untrusted input. Check identifiers
# and statuses are mapped to fixed labels before publication. Notes from
# results.tsv are intentionally ignored because they can contain internal
# hosts, repository names, migration IDs, and other detailed evidence.
#
# Required environment variables:
#
#   PR_NUMBER
#   CANDIDATE_SHA
#   JOB_STATUS
#   WORKFLOW_URL
#   CONTROL_PLANE_RESULTS
#   LIFECYCLE_RESULTS
#
# Posting additionally requires:
#
#   GITHUB_REPOSITORY
#   GH_TOKEN
#
# Usage:
#
#   post-pr-summary.sh [--render-only] [--output PATH]
#
# --render-only renders the comment without making a GitHub API request.

set -Eeuo pipefail

readonly COMMENT_MARKER='<!-- gh-elm-e2e-status -->'

RENDER_ONLY=0
OUTPUT_FILE="${RUNNER_TEMP:-/tmp}/gh-elm-e2e-pr-summary.md"
PAYLOAD_FILE=""

cleanup() {
  if [[ -n "${PAYLOAD_FILE:-}" ]]; then
    rm -f -- "$PAYLOAD_FILE"
    PAYLOAD_FILE=""
  fi

  return 0
}

trap cleanup EXIT

usage() {
  cat <<'EOF'
Usage: post-pr-summary.sh [--render-only] [--output PATH]

Render E2E results and create or update a pull request comment.

Options:
  --render-only  Render the Markdown file without posting it.
  --output PATH  Write the rendered Markdown to PATH.
  -h, --help     Show this help text.
EOF
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_variable() {
  local name="$1"

  if [[ -z "${!name:-}" ]]; then
    die "Required environment variable $name is missing."
  fi
}

require_command() {
  local name="$1"

  if ! command -v "$name" >/dev/null 2>&1; then
    die "Required command $name was not found on PATH."
  fi
}

parse_arguments() {
  while (($# > 0)); do
    case "$1" in
      --render-only)
        RENDER_ONLY=1
        shift
        ;;
      --output)
        if (($# < 2)); then
          die "--output requires a path."
        fi

        OUTPUT_FILE="$2"
        shift 2
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done
}

validate_inputs() {
  require_variable PR_NUMBER
  require_variable CANDIDATE_SHA
  require_variable JOB_STATUS
  require_variable WORKFLOW_URL
  require_variable CONTROL_PLANE_RESULTS
  require_variable LIFECYCLE_RESULTS

  if [[ ! "$PR_NUMBER" =~ ^[1-9][0-9]*$ ]]; then
    die "PR_NUMBER must be a positive integer."
  fi

  if [[ ! "$CANDIDATE_SHA" =~ ^[0-9a-fA-F]{40}$ ]]; then
    die "CANDIDATE_SHA must be a full 40-character commit SHA."
  fi

  if [[ "$WORKFLOW_URL" != https://* ]]; then
    die "WORKFLOW_URL must be an HTTPS URL."
  fi

  if [[ "$WORKFLOW_URL" =~ [[:space:]\)\(\<\>] ]]; then
    die "WORKFLOW_URL contains unsupported characters."
  fi

  if [[ -z "$OUTPUT_FILE" ]]; then
    die "The output path cannot be empty."
  fi

  if ((RENDER_ONLY == 0)); then
    require_variable GITHUB_REPOSITORY
    require_variable GH_TOKEN

    require_command gh
    require_command jq

    if [[ ! "$GITHUB_REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
      die "GITHUB_REPOSITORY must be in owner/repository format."
    fi
  fi
}

trusted_check_label() {
  local scenario_id="$1"
  local check_identifier="$2"

  # The result artifact is produced by candidate-controlled code. Never return
  # check_identifier directly. Every published label must be a fixed string
  # selected by an exact scenario-specific match.
  case "$scenario_id:$check_identifier" in
    # Checks shared by both scenarios.
    "control-plane:Configuration" | "lifecycle:Configuration")
      printf 'Configuration'
      ;;
    "control-plane:Dependencies" | "lifecycle:Dependencies")
      printf 'Dependencies'
      ;;
    "control-plane:Harness" | "lifecycle:Harness")
      printf 'Harness'
      ;;
    "control-plane:Evidence" | "lifecycle:Evidence")
      printf 'Evidence'
      ;;
    "control-plane:Migration ownership" | "lifecycle:Migration ownership")
      printf 'Migration ownership'
      ;;
    "control-plane:Preflight" | "lifecycle:Preflight")
      printf 'Preflight'
      ;;
    "control-plane:Cleanup" | "lifecycle:Cleanup")
      printf 'Cleanup'
      ;;
    "control-plane:Overall result" | "lifecycle:Overall result")
      printf 'Overall result'
      ;;

    # Control-plane checks.
    "control-plane:Create primary migration")
      printf 'Create primary migration'
      ;;
    "control-plane:Migration status")
      printf 'Migration status'
      ;;
    "control-plane:Create pagination migration")
      printf 'Create pagination migration'
      ;;
    "control-plane:List pagination")
      printf 'List pagination'
      ;;
    "control-plane:Cancel primary migration")
      printf 'Cancel primary migration'
      ;;

    # Lifecycle checks.
    "lifecycle:Create lifecycle migration")
      printf 'Create lifecycle migration'
      ;;
    "lifecycle:Initial migration status")
      printf 'Initial migration status'
      ;;
    "lifecycle:Start migration")
      printf 'Start migration'
      ;;
    "lifecycle:Target migration ID")
      printf 'Target migration ID'
      ;;
    "lifecycle:Target migration ID validation")
      printf 'Target migration ID validation'
      ;;
    "lifecycle:Wait for cutover readiness")
      printf 'Wait for cutover readiness'
      ;;
    "lifecycle:Target resources")
      printf 'Target resources'
      ;;
    "lifecycle:Initiate cutover")
      printf 'Initiate cutover'
      ;;
    "lifecycle:Wait for cutover completion")
      printf 'Wait for cutover completion'
      ;;
    "lifecycle:Revert cutover")
      printf 'Revert cutover'
      ;;
    "lifecycle:Verify reverted state")
      printf 'Verify reverted state'
      ;;
    *)
      return 1
      ;;
  esac
}

trusted_status_label() {
  local status="$1"

  # Never publish an artifact-provided status directly.
  case "$status" in
    "✅ pass" | "pass" | "success")
      printf '✅ Passed'
      ;;
    "❌ fail" | "fail" | "failure")
      printf '❌ Failed'
      ;;
    "⏭️ skip" | "skip" | "skipped")
      printf '⏭️ Skipped'
      ;;
    *)
      return 1
      ;;
  esac
}

overall_status() {
  # JOB_STATUS is provided by GitHub Actions rather than by the downloaded
  # candidate artifact.
  case "$JOB_STATUS" in
    success)
      printf '✅ Passed'
      ;;
    failure)
      printf '❌ Failed'
      ;;
    cancelled)
      printf '⏹️ Cancelled'
      ;;
    skipped)
      printf '⏭️ Skipped'
      ;;
    *)
      printf '⚠️ Unknown'
      ;;
  esac
}

append_scenario_results() {
  local scenario_id="$1"
  local scenario_name="$2"
  local results_file="$3"
  local check_identifier
  local status
  local ignored_note
  local trusted_label
  local displayed_status
  local rendered_rows=0
  local line_number=0

  {
    printf '\n'
    printf '### %s scenario\n' "$scenario_name"
    printf '\n'
    printf '| Check | Status |\n'
    printf '| --- | --- |\n'
  } >>"$OUTPUT_FILE"

  if [[ ! -s "$results_file" ]]; then
    printf '| Scenario | ⏭️ Not run |\n' >>"$OUTPUT_FILE"
    return 0
  fi

  # results.tsv has the following format:
  #
  #   check<TAB>status<TAB>note
  #
  # All fields are untrusted. The check identifier and status are used only for
  # exact matching against fixed values. The note is never rendered or logged.
  while IFS=$'\t' read -r check_identifier status ignored_note; do
    line_number=$((line_number + 1))

    if [[ -z "$check_identifier" || -z "$status" ]]; then
      die "Malformed row in $scenario_id results at line $line_number."
    fi

    if ! trusted_label="$(
      trusted_check_label "$scenario_id" "$check_identifier"
    )"; then
      # Do not print the rejected identifier because it may contain a secret.
      die "Unexpected check identifier in $scenario_id results at line $line_number."
    fi

    if ! displayed_status="$(
      trusted_status_label "$status"
    )"; then
      # Do not print the rejected status because it may contain a secret.
      die "Unexpected status in $scenario_id results at line $line_number."
    fi

    printf '| %s | %s |\n' \
      "$trusted_label" \
      "$displayed_status" \
      >>"$OUTPUT_FILE"

    rendered_rows=$((rendered_rows + 1))
  done <"$results_file"

  if ((rendered_rows == 0)); then
    printf '| Scenario | ⏭️ Not run |\n' >>"$OUTPUT_FILE"
  fi
}

render_comment() {
  local output_directory
  local temporary_output
  local final_output
  local result

  output_directory="$(dirname "$OUTPUT_FILE")"

  if [[ ! -d "$output_directory" ]]; then
    die "Output directory does not exist: $output_directory"
  fi

  temporary_output="${OUTPUT_FILE}.tmp"
  result="$(overall_status)"

  rm -f -- "$temporary_output"

  {
    printf '%s\n' "$COMMENT_MARKER"
    printf '## gh-elm E2E results\n'
    printf '\n'
    printf '**Overall result:** %s\n' "$result"
    printf '\n'
    printf -- '- Candidate: `%s`\n' "$CANDIDATE_SHA"
    printf -- '- Workflow run: [View logs](%s)\n' "$WORKFLOW_URL"
  } >"$temporary_output"

  # Point the scenario renderer at the temporary file so the complete report
  # can be moved into place atomically.
  final_output="$OUTPUT_FILE"
  OUTPUT_FILE="$temporary_output"

  append_scenario_results \
    "control-plane" \
    "Control-plane" \
    "$CONTROL_PLANE_RESULTS"

  append_scenario_results \
    "lifecycle" \
    "Lifecycle" \
    "$LIFECYCLE_RESULTS"

  OUTPUT_FILE="$final_output"

  if ! mv -- "$temporary_output" "$OUTPUT_FILE"; then
    rm -f -- "$temporary_output"
    die "Failed to finalize the rendered comment."
  fi

  printf 'Rendered E2E pull request summary: %s\n' "$OUTPUT_FILE"
}

find_existing_comment_id() {
  local comment_ids
  local comment_id

  # Pull request comments use the issues comments API. Match both the Actions
  # bot identity and the hidden marker so unrelated bot comments are ignored.
  if ! comment_ids="$(
    gh api \
      --paginate \
      "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" \
      --jq '.[] |
        select(.user.login == "github-actions[bot]") |
        select(.body | contains("<!-- gh-elm-e2e-status -->")) |
        .id'
  )"; then
    die "Failed to retrieve comments for pull request #$PR_NUMBER."
  fi

  # Use the first matching comment if an earlier implementation accidentally
  # created more than one.
  comment_id="${comment_ids%%$'\n'*}"

  if [[ -n "$comment_id" && ! "$comment_id" =~ ^[1-9][0-9]*$ ]]; then
    die "GitHub returned an invalid existing comment ID."
  fi

  printf '%s' "$comment_id"
}

write_request_payload() {
  local payload_file="$1"

  if ! jq -n \
    --rawfile body "$OUTPUT_FILE" \
    '{body: $body}' >"$payload_file"; then
    die "Failed to construct the GitHub comment request."
  fi
}

post_comment() {
  local comment_id

  PAYLOAD_FILE="$(mktemp)"

  write_request_payload "$PAYLOAD_FILE"
  comment_id="$(find_existing_comment_id)"

  if [[ -n "$comment_id" ]]; then
    if ! gh api \
      --method PATCH \
      "repos/$GITHUB_REPOSITORY/issues/comments/$comment_id" \
      --input "$PAYLOAD_FILE" \
      >/dev/null; then
      die "Failed to update E2E comment $comment_id on pull request #$PR_NUMBER."
    fi

    printf 'Updated E2E summary comment %s on pull request #%s.\n' \
      "$comment_id" \
      "$PR_NUMBER"
  else
    if ! gh api \
      --method POST \
      "repos/$GITHUB_REPOSITORY/issues/$PR_NUMBER/comments" \
      --input "$PAYLOAD_FILE" \
      >/dev/null; then
      die "Failed to create an E2E comment on pull request #$PR_NUMBER."
    fi

    printf 'Created E2E summary comment on pull request #%s.\n' \
      "$PR_NUMBER"
  fi

  rm -f -- "$PAYLOAD_FILE"
  PAYLOAD_FILE=""
}

main() {
  parse_arguments "$@"
  validate_inputs
  render_comment

  if ((RENDER_ONLY == 1)); then
    printf 'Render-only mode enabled; no pull request comment was posted.\n'
    return 0
  fi

  post_comment
}

main "$@"
