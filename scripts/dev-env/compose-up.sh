#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

configure_compose_no_proxy

require_cmd docker
require_cmd curl
begin_env_lock compose
trap 'end_env_lock compose' EXIT

ensure_state_dirs
ensure_local_images
generate_compose_certs
ensure_compose_ssh_keys
ensure_secrets_master_key compose
write_compose_env
write_cli_env compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
if [ "${AXERN_COMPOSE_RESET_STATE:-0}" = "1" ] || [ "${AXERN_COMPOSE_RESET_STATE:-0}" = "true" ]; then
  compose_project_reset_state
fi
compose_project_up

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
if [ "${AXERN_SKIP_COMPOSE_RUNTIME_IMAGE_IMPORTS:-0}" = "1" ] || [ "${AXERN_SKIP_COMPOSE_RUNTIME_IMAGE_IMPORTS:-0}" = "true" ]; then
  echo "compose_runtime_image_imports_skipped=true"
else
  IMAGE="${PYTHON311_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
  IMAGE="${SERVER_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
  IMAGE="${CODING_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
  IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
  IMAGE="${CLAUDE_CODE_BUNDLE_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
  IMAGE="${CODEX_BUNDLE_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"
fi

echo "compose_up_ok=true"
echo "cli_env=$(cli_env_file compose)"
echo "axern_config=$(axern_config_file)"
