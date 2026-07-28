#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE="${AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE:-0}"
export AXERN_CLI_E2E_REBUILD_RUNTIME_IMAGE="${AXERN_CLI_E2E_REBUILD_RUNTIME_IMAGE:-0}"

source "${SCRIPT_DIR}/lib.sh"
source "${SCRIPT_DIR}/environment.sh"
source "${SCRIPT_DIR}/image_ref.sh"

reserve_e2e_ports
trap cleanup EXIT
trap 'axern_cli_e2e_failed=1; failed_e2e_step="${current_e2e_step:-unknown}"' ERR

run_step setup_e2e_environment setup_e2e_environment
run_step external_image_ref verify_external_image_ref

echo "axern_cli_image_ref_e2e_ok=true"
