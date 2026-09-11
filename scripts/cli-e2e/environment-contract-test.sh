#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

python3 - "${AXERN_ROOT}/scripts/cli-e2e/environment.sh" <<'PY'
import pathlib
import sys

environment = pathlib.Path(sys.argv[1]).read_text()
start = environment.index('docker run -d \\\n    --name "${NODE_CONTAINER_NAME}"')
end = environment.index('    "${IMAGE_TAG}" \\\n', start)
node_run = environment[start:end]

for required in (
    '--add-host "host.docker.internal:host-gateway"',
    '-e "AXNODED_CONTROL_PLANE_TARGET=host.docker.internal:${CONTROLD_GRPC_ADDRESS##*:}"',
):
    if required not in node_run:
        raise SystemExit(f"CLI E2E node container is missing host control-plane routing: {required}")

if '-grpc-address "0.0.0.0:${CONTROLD_GRPC_ADDRESS##*:}"' not in environment:
    raise SystemExit("CLI E2E controld must listen beyond host loopback for the node container")
PY

echo "cli_e2e_environment_contract_ok=true"
