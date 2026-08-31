#!/usr/bin/env bash
#
# Control-plane E2E entrypoint for the gh-elm CLI against a protected GHES
# migration environment.
#
# The workflow builds, installs, and verifies the candidate before invoking
# this script. The control-plane scenario exercises migration creation, status
# retrieval, list pagination, explicit cancellation, and run-owned cleanup.
#
# Shared functionality lives under script/e2e/lib/. The scenario implementation
# lives in script/e2e/scenarios/control-plane.sh.

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

# shellcheck source=script/e2e/lib/cleanup.sh
source "$SCRIPT_DIR/lib/cleanup.sh"

# shellcheck source=script/e2e/scenarios/control-plane.sh
source "$SCRIPT_DIR/scenarios/control-plane.sh"

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

  record_result \
    "Configuration" \
    "✅ pass" \
    "Required control-plane E2E configuration is present."

  # The workflow has already built, installed, and verified gh elm for this
  # job. Scenario execution uses that existing installation.
  run_preflight

  if ! declare -F run_scenario >/dev/null 2>&1; then
    fail \
      "Harness" \
      "The control-plane scenario did not define the required run_scenario function."
  fi

  run_scenario "$@"

  log "Control-plane E2E scenario assertions completed."
}

main "$@"
