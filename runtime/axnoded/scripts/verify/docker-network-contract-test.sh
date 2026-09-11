#!/usr/bin/env bash
set -euo pipefail

AXNODED_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

python3 - \
  "${AXNODED_DIR}/scripts/demo/run-dashboard-nginx-demo-in-container.sh" \
  "${AXNODED_DIR}/scripts/demo/run-dashboard-nginx-demo.sh" \
  "${AXNODED_DIR}/scripts/verify/verify-bpfnetctl-e2e.sh" \
  "${AXNODED_DIR}/scripts/verify/node-all-in-one-entrypoint.sh" \
  "${AXNODED_DIR}/scripts/verify/verify-node-python-runtime-e2e.sh" \
  "${AXNODED_DIR}/scripts/verify/verify-node-service-probes-e2e.sh" \
  "${AXNODED_DIR}/scripts/verify/verify-node-service-volumes-e2e.sh" \
  "${AXNODED_DIR}/scripts/lib/verify-docker-common.sh" <<'PY'
import ipaddress
import pathlib
import re
import sys

docker_bridge = ipaddress.ip_network("172.17.0.0/16")


def assert_isolated_default(path):
    text = path.read_text()
    node_range = re.search(
        r'^(?:export )?AXNODED_NETWORK_IP_RANGE="\$\{AXNODED_NETWORK_IP_RANGE:-([^}]+)\}"$',
        text,
        re.MULTILINE,
    )
    if node_range is None:
        raise SystemExit(f"{path.name} must define an overridable node sandbox network range")
    if ipaddress.ip_interface(node_range.group(1)).network.overlaps(docker_bridge):
        raise SystemExit(f"{path.name} node sandbox network must not overlap Docker's default bridge")


dashboard_inner = pathlib.Path(sys.argv[1])
assert_isolated_default(dashboard_inner)
if 'ip_range = "${AXNODED_NETWORK_IP_RANGE}"' not in dashboard_inner.read_text():
    raise SystemExit("dashboard axnoded config must use AXNODED_NETWORK_IP_RANGE")

for path in map(pathlib.Path, sys.argv[2:4]):
    if '-e AXNODED_NETWORK_IP_RANGE' not in path.read_text():
        raise SystemExit(f"{path.name} must pass the dashboard node network override")

assert_isolated_default(pathlib.Path(sys.argv[4]))

python_runtime = pathlib.Path(sys.argv[5])
python_runtime_text = python_runtime.read_text()
for fragment in (
    '--network "${POSTGRES_NETWORK_NAME}"',
    'AXNODED_CONTROL_PLANE_TARGET=controld:${CONTROLD_GRPC_PORT}',
    'AXNODED_CONTROL_PLANE_NODE_TARGET=${NODE_CONTAINER_NAME}:${NODE_GRPC_PORT}',
):
    if fragment not in python_runtime_text:
        raise SystemExit(f"{python_runtime.name} must use its shared Docker network: {fragment}")

for path in map(pathlib.Path, sys.argv[6:8]):
    text = path.read_text()
    if '--add-host "host.docker.internal:host-gateway"' not in text:
        raise SystemExit(f"{path.name} must map the Linux host gateway for its control plane")
    if '-grpc-address "0.0.0.0:' not in text:
        raise SystemExit(f"{path.name} must expose controld on the Linux host gateway")

docker_common = pathlib.Path(sys.argv[8]).read_text()
for fragment in (
    'registry_runtime_host="${registry_ip}:5000"',
    'LOCAL_REGISTRY_CLUSTER_HOST="${registry_runtime_host}"',
    'REGISTRY_NO_PROXY="${REGISTRY_NO_PROXY:+${REGISTRY_NO_PROXY},}${registry_ip}"',
):
    if fragment not in docker_common:
        raise SystemExit(
            "Nydus local-build verification must use the registry container bridge endpoint: "
            + fragment
        )
PY

echo "docker_network_contract_ok=true"
