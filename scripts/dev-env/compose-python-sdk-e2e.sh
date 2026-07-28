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

runtime_list="${AXERN_PYTHON_SDK_E2E_RUNTIMES:-runsc runc}"
for runtime_class in ${runtime_list}; do
  echo "python_sdk_sandbox_e2e_runtime=${runtime_class} phase=start"
  uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/sandbox_tunnel_e2e.py" \
    --endpoint "${AXERN_ENDPOINT}" \
    --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
    --tls-cert "${AXERN_TLS_CERT}" \
    --tls-key "${AXERN_TLS_KEY}" \
    --runtime-class "${runtime_class}" \
    --node-container "${node_container}"
done

echo "compose_python_sdk_e2e_ok=true"
