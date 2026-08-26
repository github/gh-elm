#!/usr/bin/env bash
#
# E2E scenario entrypoint for the gh-elm CLI against a protected GHES migration
# environment.
#
# The workflow builds, installs, and verifies the candidate once before invoking
# this script sequentially for each supported scenario:
#
#   control-plane
#     Exercises migration creation, status retrieval, list pagination,
#     cancellation, and run-owned cleanup.
#
#   lifecycle
#     Exercises migration creation, start, target-side APIs, cutover, cutover
#     completion, revert, and post-revert verification.
#
# Shared functionality lives under script/e2e/lib/. Each scenario defines a
# run_scenario function under script/e2e/scenarios/.

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

# Only this entrypoint sources the harness modules. Individual library and
# scenario files must not source one another.

# shellcheck source=script/e2e/lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"

# shellcheck source=script/e2e/lib/evidence.sh
source "$SCRIPT_DIR/lib/evidence.sh"

# shellcheck source=script/e2e/lib/configuration.sh
source "$SCRIPT_DIR/lib/configuration.sh"

# shellcheck source=script/e2e/lib/ownership.sh
source "$SCRIPT_DIR/lib/ownership.sh"

# shellcheck source=script/e2e/lib/migration.sh
source "$SCRIPT_DIR/lib/migration.sh"

# shellcheck source=script/e2e/lib/polling.sh
source "$SCRIPT_DIR/lib/polling.sh"

# shellcheck source=script/e2e/lib/target.sh
source "$SCRIPT_DIR/lib/target.sh"

# shellcheck source=script/e2e/lib/cleanup.sh
source "$SCRIPT_DIR/lib/cleanup.sh"

load_scenario() {
  case "$E2E_MODE" in
    control-plane)
      # shellcheck source=script/e2e/scenarios/control-plane.sh
      source "$SCRIPT_DIR/scenarios/control-plane.sh"
      ;;
    lifecycle)
      # shellcheck source=script/e2e/scenarios/lifecycle.sh
      source "$SCRIPT_DIR/scenarios/lifecycle.sh"
      ;;
    *)
      # validate_configuration should reject unsupported modes before this
      # function runs. Keep this guard so E2E_MODE can never be used to derive
      # an arbitrary source path.
      fail \
        "Configuration" \
        "Unsupported E2E_MODE: $E2E_MODE. Expected control-plane or lifecycle."
      ;;
  esac

  if ! declare -F run_scenario >/dev/null 2>&1; then
    fail \
      "Harness" \
      "Scenario $E2E_MODE did not define the required run_scenario function."
  fi
}

main() {
  # Create evidence before validation or external operations so early failures
  # can still be recorded and uploaded by the workflow.
  initialize_harness

  # Cleanup preserves the original process status while attempting recovery for
  # every migration recorded as owned by this scenario.
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  validate_configuration
  configure_runtime
  write_evidence_header
  load_scenario

  record_result \
    "Configuration" \
    "✅ pass" \
    "Required $E2E_MODE E2E configuration is present."

  # The workflow has already built, installed, and verified gh elm once for
  # this job. Scenario execution uses that existing installation.
  run_preflight

  run_scenario "$@"

  log "$E2E_MODE E2E scenario assertions completed."
}

main "$@"
