#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib.sh"
source "${SCRIPT_DIR}/environment.sh"
source "${SCRIPT_DIR}/catalog_namespace_quota.sh"
source "${SCRIPT_DIR}/quota_admission.sh"
source "${SCRIPT_DIR}/base_environment.sh"
source "${SCRIPT_DIR}/ssh_gateway.sh"
source "${SCRIPT_DIR}/service_rollout.sh"
source "${SCRIPT_DIR}/service_volume.sh"
source "${SCRIPT_DIR}/run.sh"
source "${SCRIPT_DIR}/admin_lifecycle.sh"

reserve_e2e_ports
trap cleanup EXIT
trap 'axern_cli_e2e_failed=1; failed_e2e_step="${current_e2e_step:-unknown}"' ERR

run_step setup_e2e_environment setup_e2e_environment
run_step catalog_namespace_quota verify_catalog_namespace_quota
run_step quota_admission verify_quota_admission
run_step base_environment create_base_environment
run_step ssh_gateway verify_ssh_gateway
run_step service_rollout verify_secret_environment_service_rollout
run_step service_volume verify_service_volume
run_step run verify_run
run_step admin_lifecycle verify_admin_lifecycle

echo "axern_cli_e2e_ok=true"
