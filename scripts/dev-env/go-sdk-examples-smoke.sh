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

export AXERN_CONTEXT=compose
export AXERN_RUNTIME_CLASS="${AXERN_GO_SDK_EXAMPLES_RUNTIME:-runsc}"

for example in basic process files; do
  echo "go_sdk_example=${example} phase=start"
  "${go_bin}" run "./sdk/go/examples/${example}"
  echo "go_sdk_example=${example} ok=true"
done

echo "go_sdk_examples_smoke_ok=true"
