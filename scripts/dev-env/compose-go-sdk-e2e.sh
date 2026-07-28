#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
go_bin="$(axern_go_bin)"
require_cmd "${go_bin}"

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

runtime_list="${AXERN_GO_SDK_E2E_RUNTIMES:-runsc runc}"
for runtime_class in ${runtime_list}; do
  echo "go_sdk_sandbox_e2e_runtime=${runtime_class} phase=start"
  "${go_bin}" run ./sdk/go/tests/e2e \
    --endpoint "${AXERN_ENDPOINT}" \
    --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
    --tls-cert "${AXERN_TLS_CERT}" \
    --tls-key "${AXERN_TLS_KEY}" \
    --runtime-class "${runtime_class}"
done

echo "compose_go_sdk_e2e_ok=true"
