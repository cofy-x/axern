#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

python3 - \
  "${AXERN_ROOT}/scripts/cli-e2e/environment.sh" \
  "${AXERN_ROOT}/scripts/cli-e2e/lib.sh" <<'PY'
import ipaddress
import pathlib
import re
import sys

environment = pathlib.Path(sys.argv[1]).read_text()
library = pathlib.Path(sys.argv[2]).read_text()
start = environment.index('docker run -d \\\n    --name "${NODE_CONTAINER_NAME}"')
end = environment.index('    "${IMAGE_TAG}" \\\n', start)
node_run = environment[start:end]

for required in (
    '--add-host "host.docker.internal:host-gateway"',
    '-e "AXNODED_NETWORK_IP_RANGE=${AXNODED_NETWORK_IP_RANGE}"',
    '-e "AXNODED_CONTROL_PLANE_TARGET=host.docker.internal:${CONTROLD_GRPC_ADDRESS##*:}"',
):
    if required not in node_run:
        raise SystemExit(f"CLI E2E node container is missing host control-plane routing: {required}")

if '-grpc-address "0.0.0.0:${CONTROLD_GRPC_ADDRESS##*:}"' not in environment:
    raise SystemExit("CLI E2E controld must listen beyond host loopback for the node container")
if 'if ! node_control_plane_tcp_ready; then' not in environment:
    raise SystemExit("CLI E2E must fail fast when the node cannot reach the host control plane")

node_range = re.search(
    r'^AXNODED_NETWORK_IP_RANGE="\$\{AXNODED_NETWORK_IP_RANGE:-([^}]+)\}"$',
    library,
    re.MULTILINE,
)
if node_range is None:
    raise SystemExit("CLI E2E must define an overridable node sandbox network range")
if ipaddress.ip_interface(node_range.group(1)).network.overlaps(
    ipaddress.ip_network("172.17.0.0/16")
):
    raise SystemExit("CLI E2E node sandbox network must not overlap Docker's default bridge")
PY

echo "cli_e2e_environment_contract_ok=true"
