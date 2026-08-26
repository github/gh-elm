#!/usr/bin/env bash
#
# Configuration validation and runtime setup for the gh-elm E2E harness.
#
# This file is sourced by script/e2e/test-elm-ghes.sh and is not intended to
# be executed directly.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  printf 'This file must be sourced by script/e2e/test-elm-ghes.sh.\n' >&2
  exit 1
fi

validate_single_line_variable() {
  local name="$1"
  local value="${!name:-}"

  if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    fail \
      "Configuration" \
      "$name must not contain line breaks."
  fi
}

validate_http_url() {
  local name="$1"
  local value="${!name:-}"

  validate_single_line_variable "$name"

  case "$value" in
    http://* | https://*)
      ;;
    *)
      fail \
        "Configuration" \
        "$name must be an absolute HTTP or HTTPS URL, not $value."
      ;;
  esac

  # Credentials must be supplied separately through token variables. Embedded
  # URL credentials could be exposed in evidence, logs, or error messages.
  if [[ "$value" =~ ^https?://[^/]*@ ]]; then
    fail \
      "Configuration" \
      "$name must not contain embedded credentials."
  fi
}

validate_identifier() {
  local name="$1"
  local value="${!name:-}"

  validate_single_line_variable "$name"

  if [[ "$value" == */* ]]; then
    fail \
      "Configuration" \
      "$name must be a single organization or repository name, not $value."
  fi

  if [[ "$value" == "." || "$value" == ".." ]]; then
    fail \
      "Configuration" \
      "$name cannot be $value."
  fi

  if [[ "$value" =~ [[:cntrl:]] ]]; then
    fail \
      "Configuration" \
      "$name must not contain control characters."
  fi
}

validate_token() {
  local name="$1"

  validate_single_line_variable "$name"

  # Do not include the token value in any validation error.
  if [[ "${!name}" =~ [[:space:]] ]]; then
    fail \
      "Configuration" \
      "$name must not contain whitespace."
  fi
}

validate_configuration() {
  local variable
  local dependency
  local value
  local normalized_value
  local maximum_value
  local lifecycle_polling_budget

  for variable in \
    SOURCE_HOST \
    SOURCE_TOKEN \
    SOURCE_ORG \
    SOURCE_REPO \
    TARGET_HOST \
    TARGET_TOKEN \
    TARGET_ORG \
    TARGET_VISIBILITY \
    E2E_RUN_ID; do
    require_variable "$variable"
  done

  for dependency in \
    gh \
    jq \
    sed \
    tr \
    sleep \
    date; do
    require_command "$dependency"
  done

  case "$E2E_MODE" in
    control-plane | lifecycle)
      ;;
    *)
      fail \
        "Configuration" \
        "Unsupported E2E_MODE: $E2E_MODE. Expected control-plane or lifecycle."
      ;;
  esac

  case "$TARGET_VISIBILITY" in
    private | internal)
      ;;
    *)
      fail \
        "Configuration" \
        "TARGET_VISIBILITY must be private or internal, not $TARGET_VISIBILITY."
      ;;
  esac

  # Validate and bound timeout strings before using Bash arithmetic. Bash uses
  # fixed-width signed integers, so evaluating an unbounded digits-only value
  # could overflow before a later maximum-value check is reached.
  for variable in \
    E2E_POLL_INTERVAL_SECONDS \
    E2E_STATE_TIMEOUT_SECONDS \
    E2E_CUTOVER_TIMEOUT_SECONDS; do
    value="${!variable:-}"

    if [[ ! "$value" =~ ^[0-9]+$ ]]; then
      fail \
        "Configuration" \
        "$variable must be a positive integer, not ${value:-<empty>}."
    fi

    # Remove leading zeroes without first interpreting the value as an integer.
    normalized_value="$(
      printf '%s' "$value" |
        sed -E 's/^0+//'
    )"

    if [[ -z "$normalized_value" ]]; then
      fail \
        "Configuration" \
        "$variable must be a positive integer, not $value."
    fi

    case "$variable" in
      E2E_POLL_INTERVAL_SECONDS)
        maximum_value=300
        ;;
      E2E_STATE_TIMEOUT_SECONDS | E2E_CUTOVER_TIMEOUT_SECONDS)
        maximum_value=2700
        ;;
    esac

    # Compare string lengths first. Arithmetic is safe only after proving that
    # the normalized value contains no more digits than the small upper bound.
    if ((${#normalized_value} > ${#maximum_value})) ||
      ((${#normalized_value} == ${#maximum_value} &&
        10#$normalized_value > maximum_value)); then
      fail \
        "Configuration" \
        "$variable must not exceed $maximum_value seconds."
    fi

    # The value is now known to be small enough for safe decimal conversion.
    printf -v "$variable" '%d' "$((10#$normalized_value))"
  done

  if ((E2E_STATE_TIMEOUT_SECONDS < E2E_POLL_INTERVAL_SECONDS)); then
    fail \
      "Configuration" \
      "E2E_STATE_TIMEOUT_SECONDS must be at least E2E_POLL_INTERVAL_SECONDS."
  fi

  if ((E2E_CUTOVER_TIMEOUT_SECONDS < E2E_POLL_INTERVAL_SECONDS)); then
    fail \
      "Configuration" \
      "E2E_CUTOVER_TIMEOUT_SECONDS must be at least E2E_POLL_INTERVAL_SECONDS."
  fi

  # Lifecycle can use each timeout twice. Both values are already bounded at
  # 2700 seconds, so this addition cannot overflow. Requiring their sum to be
  # at most 2700 is equivalent to limiting the four lifecycle polling phases
  # to an aggregate budget of 5400 seconds.
  lifecycle_polling_budget=$((
    E2E_STATE_TIMEOUT_SECONDS +
    E2E_CUTOVER_TIMEOUT_SECONDS
  ))

  if ((lifecycle_polling_budget > 2700)); then
    fail \
      "Configuration" \
      "E2E_STATE_TIMEOUT_SECONDS plus E2E_CUTOVER_TIMEOUT_SECONDS must not exceed 2700 seconds (5400 seconds across the four lifecycle polling phases)."
  fi

  validate_http_url SOURCE_HOST
  validate_http_url TARGET_HOST

  validate_token SOURCE_TOKEN
  validate_token TARGET_TOKEN

  validate_identifier SOURCE_ORG
  validate_identifier SOURCE_REPO
  validate_identifier TARGET_ORG

  validate_single_line_variable E2E_RUN_ID
  validate_single_line_variable TARGET_VISIBILITY

  if [[ "$E2E_RUN_ID" == -* ]]; then
    fail \
      "Configuration" \
      "E2E_RUN_ID cannot begin with '-'."
  fi
}

configure_runtime() {
  local config_root
  local repository_prefix="gh-elm-e2e-"
  local primary_suffix="-primary"
  local pagination_suffix="-page"
  local maximum_repository_length=100
  local longest_suffix_length
  local maximum_safe_run_id_length

  # Normalize host URLs once so all commands, metadata, and evidence use the
  # same representation.
  while [[ "$SOURCE_HOST" == */ ]]; do
    SOURCE_HOST="${SOURCE_HOST%/}"
  done

  while [[ "$TARGET_HOST" == */ ]]; do
    TARGET_HOST="${TARGET_HOST%/}"
  done

  if [[ "$SOURCE_HOST" == "http:" || "$SOURCE_HOST" == "https:" ]]; then
    fail \
      "Configuration" \
      "SOURCE_HOST must include a hostname."
  fi

  if [[ "$TARGET_HOST" == "http:" || "$TARGET_HOST" == "https:" ]]; then
    fail \
      "Configuration" \
      "TARGET_HOST must include a hostname."
  fi

  SAFE_RUN_ID="$(
    printf '%s' "$E2E_RUN_ID" |
      tr '[:upper:]' '[:lower:]' |
      sed -E \
        -e 's/[^a-z0-9._-]+/-/g' \
        -e 's/^-+//' \
        -e 's/-+$//'
  )"

  if [[ -z "$SAFE_RUN_ID" ]]; then
    fail \
      "Configuration" \
      "E2E_RUN_ID did not contain any usable repository-name characters."
  fi

  # Calculate the maximum run-ID length using the longest generated repository
  # suffix. This ensures every generated repository name fits within the
  # repository-name limit.
  if ((${#primary_suffix} >= ${#pagination_suffix})); then
    longest_suffix_length="${#primary_suffix}"
  else
    longest_suffix_length="${#pagination_suffix}"
  fi

  maximum_safe_run_id_length=$((
    maximum_repository_length -
    ${#repository_prefix} -
    longest_suffix_length
  ))

  if ((maximum_safe_run_id_length <= 0)); then
    fail \
      "Configuration" \
      "Generated repository prefix and suffix leave no room for E2E_RUN_ID."
  fi

  if ((${#SAFE_RUN_ID} > maximum_safe_run_id_length)); then
    SAFE_RUN_ID="${SAFE_RUN_ID:0:maximum_safe_run_id_length}"

    # Avoid ending the truncated component with a separator.
    SAFE_RUN_ID="$(
      printf '%s' "$SAFE_RUN_ID" |
        sed -E 's/[-._]+$//'
    )"
  fi

  if [[ -z "$SAFE_RUN_ID" ]]; then
    fail \
      "Configuration" \
      "E2E_RUN_ID became empty after repository-name normalization."
  fi

  TARGET_REPO_PRIMARY="${repository_prefix}${SAFE_RUN_ID}${primary_suffix}"
  TARGET_REPO_PAGINATION="${repository_prefix}${SAFE_RUN_ID}${pagination_suffix}"

  if ((${#TARGET_REPO_PRIMARY} > maximum_repository_length)); then
    fail \
      "Configuration" \
      "Generated primary repository name exceeds $maximum_repository_length characters."
  fi

  if ((${#TARGET_REPO_PAGINATION} > maximum_repository_length)); then
    fail \
      "Configuration" \
      "Generated pagination repository name exceeds $maximum_repository_length characters."
  fi

  # gh-elm reads these variables in non-interactive environments.
  export GH_SOURCE_HOST="$SOURCE_HOST"
  export GH_SOURCE_TOKEN="$SOURCE_TOKEN"
  export GH_TARGET_HOST="$TARGET_HOST"
  export GH_TARGET_TOKEN="$TARGET_TOKEN"

  # Keep temporary gh-elm configuration outside the uploaded evidence
  # directory. GH_CONFIG_DIR deliberately remains unchanged so the candidate
  # installed by the workflow remains registered for subsequent gh elm calls.
  # The workflow gives each scenario a unique E2E_RUN_ID, so their gh-elm
  # configuration directories do not overlap.
  config_root="${RUNNER_TEMP:-/tmp}/gh-elm-e2e-$SAFE_RUN_ID"

  if [[ -e "$config_root" && ! -d "$config_root" ]]; then
    fail \
      "Configuration" \
      "Temporary configuration path exists but is not a directory: $config_root."
  fi

  # Restrict file-backed credentials and configuration created by gh-elm.
  umask 077

  export GH_ELM_CONFIG_DIR="$config_root/elm"
  export GH_ELM_CREDENTIAL_STORE=file
  export GH_PROMPT_DISABLED=1
  export NO_COLOR=1

  if ! mkdir -p "$GH_ELM_CONFIG_DIR"; then
    fail \
      "Configuration" \
      "Failed to create the temporary gh-elm configuration directory."
  fi

  if ! chmod 700 "$config_root" "$GH_ELM_CONFIG_DIR"; then
    fail \
      "Configuration" \
      "Failed to restrict temporary gh-elm configuration directory permissions."
  fi

  log "Configured runtime for E2E scenario $E2E_MODE."
  log "Primary target repository: $TARGET_ORG/$TARGET_REPO_PRIMARY"

  if [[ "$E2E_MODE" == "control-plane" ]]; then
    log "Pagination target repository: $TARGET_ORG/$TARGET_REPO_PAGINATION"
  fi
}
