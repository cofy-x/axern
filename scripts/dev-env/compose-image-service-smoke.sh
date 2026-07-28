#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd curl
require_cmd docker
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
run_local_image_service_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" compose-image-service "http://127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}"
