#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd uv

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

if ! docker image inspect "${DESKTOP_BASE_RUNTIME_IMAGE}" >/dev/null 2>&1; then
  echo "missing desktop runtime image ${DESKTOP_BASE_RUNTIME_IMAGE}; run make local-images-build first" >&2
  exit 1
fi
IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"

uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/computer_use_e2e.py" \
  --endpoint "${AXERN_ENDPOINT}" \
  --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
  --tls-cert "${AXERN_TLS_CERT}" \
  --tls-key "${AXERN_TLS_KEY}" \
  --node-container "${node_container}"

echo "compose_computer_use_e2e_ok=true"
