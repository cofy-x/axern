#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
AXNODED_HTTP_ADDRESS="${AXNODED_HTTP_ADDRESS:-0.0.0.0:23001}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axnoded-node-execution-envelope-prewarm-e2e}"
AXNODED_IDLE_RUNTIME_RETENTION_TTL="${AXNODED_IDLE_RUNTIME_RETENTION_TTL:-30s}"
AXNODED_IDLE_RUNTIME_RETENTION_MAX="${AXNODED_IDLE_RUNTIME_RETENTION_MAX:-8}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

dump_logs() {
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 160 /var/log/axnoded/axnoded.log >&2 || true
}

cleanup() {
  docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_verify_image
cleanup

docker run -d \
  --name "${NODE_CONTAINER_NAME}" \
  --privileged \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "REGISTRY_PROXY_URL=${REGISTRY_PROXY_URL}" \
  -e "REGISTRY_NO_PROXY=${REGISTRY_NO_PROXY}" \
  -e "AXNODED_HTTP_ADDRESS=${AXNODED_HTTP_ADDRESS}" \
  -e "AXNODED_IDLE_RUNTIME_RETENTION_TTL=${AXNODED_IDLE_RUNTIME_RETENTION_TTL}" \
  -e "AXNODED_IDLE_RUNTIME_RETENTION_MAX=${AXNODED_IDLE_RUNTIME_RETENTION_MAX}" \
  "${IMAGE_TAG}" \
  /bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >/dev/null

deadline=$((SECONDS + 180))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if ! docker inspect -f '{{.State.Running}}' "${NODE_CONTAINER_NAME}" 2>/dev/null | grep -qx true; then
    echo "node container exited before becoming ready" >&2
    dump_logs
    exit 1
  fi
  if docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${AXNODED_SOCKET}' && curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
    break
  fi
  sleep 2
done

if ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${AXNODED_SOCKET}' && curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
  echo "node container did not become ready in time" >&2
  dump_logs
  exit 1
fi

if ! docker exec \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  "${NODE_CONTAINER_NAME}" \
  /bin/bash /workspace/scripts/verify/verify-node-execution-envelope-prewarm-e2e-in-container.sh; then
  dump_logs
  exit 1
fi

echo "verify_node_execution_envelope_prewarm_e2e_host_ok=true"
