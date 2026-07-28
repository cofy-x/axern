#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd uv

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose --function-smoke
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

uv run --package axern-sdk python "${AXERN_ROOT}/scripts/dev-env/smoke/function_invoke.py" \
  --endpoint "${AXERN_ENDPOINT}" \
  --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
  --tls-cert "${AXERN_TLS_CERT}" \
  --tls-key "${AXERN_TLS_KEY}"

echo "compose_function_smoke_ok=true"
