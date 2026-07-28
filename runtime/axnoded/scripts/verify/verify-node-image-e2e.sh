#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

E2E_KIND="${E2E_KIND:?E2E_KIND is required}"
IMAGE_URL="${IMAGE_URL:?IMAGE_URL is required}"
EXPECT_MOUNT_TYPE="${EXPECT_MOUNT_TYPE:?EXPECT_MOUNT_TYPE is required}"
PROBE_PATH="${PROBE_PATH:?PROBE_PATH is required}"
VERIFY_ARGV_JSON="${VERIFY_ARGV_JSON:?VERIFY_ARGV_JSON is required}"

VERIFY_EXPECT_STDOUT="${VERIFY_EXPECT_STDOUT:-}"
VERIFY_EXPECT_STDERR="${VERIFY_EXPECT_STDERR:-}"
VERIFY_EXPECT_EXIT="${VERIFY_EXPECT_EXIT:-0}"

IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
AXNODED_HTTP_ADDRESS="${AXNODED_HTTP_ADDRESS:-0.0.0.0:23001}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axnoded-node-${E2E_KIND}-e2e}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

OCI_TEST_INSECURE_REGISTRIES="${IMAGEMGR_INSECURE_REGISTRIES:-${OCI_TEST_INSECURE_REGISTRIES:-}}"
if [ "${E2E_KIND}" = "oci" ]; then
  prepare_oci_test_image_source "${IMAGE_URL}"
  IMAGE_URL="${PREPARED_OCI_TEST_IMAGE}"
fi

dump_logs() {
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 120 /var/log/axnoded/axnoded.log >&2 || true
  echo "--- imagemgr log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 120 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
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
  -e "IMAGEMGR_SOCKET=${IMAGEMGR_SOCKET}" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "REGISTRY_PROXY_URL=${REGISTRY_PROXY_URL}" \
  -e "REGISTRY_NO_PROXY=${REGISTRY_NO_PROXY}" \
  -e "IMAGEMGR_INSECURE_REGISTRIES=${OCI_TEST_INSECURE_REGISTRIES}" \
  -e "AXNODED_HTTP_ADDRESS=${AXNODED_HTTP_ADDRESS}" \
  -e "AXNODED_IDLE_RUNTIME_RETENTION_TTL=0s" \
  -e "AXNODED_IDLE_RUNTIME_RETENTION_MAX=0" \
  "${IMAGE_TAG}" \
  /bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >/dev/null

deadline=$((SECONDS + 180))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if ! docker inspect -f '{{.State.Running}}' "${NODE_CONTAINER_NAME}" 2>/dev/null | grep -qx true; then
    echo "node container exited before becoming ready" >&2
    dump_logs
    exit 1
  fi
  if docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${IMAGEMGR_SOCKET}' && test -S '${AXNODED_SOCKET}' && curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
    break
  fi
  sleep 2
done

if ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${IMAGEMGR_SOCKET}' && test -S '${AXNODED_SOCKET}' && curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
  echo "node container did not become ready in time" >&2
  dump_logs
  exit 1
fi

if ! docker exec \
  -e "IMAGEMGR_SOCKET=${IMAGEMGR_SOCKET}" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "IMAGE_URL=${IMAGE_URL}" \
  -e "EXPECT_MOUNT_TYPE=${EXPECT_MOUNT_TYPE}" \
  -e "PROBE_PATH=${PROBE_PATH}" \
  -e "VERIFY_ARGV_JSON=${VERIFY_ARGV_JSON}" \
  -e "VERIFY_EXPECT_STDOUT=${VERIFY_EXPECT_STDOUT}" \
  -e "VERIFY_EXPECT_STDERR=${VERIFY_EXPECT_STDERR}" \
  -e "VERIFY_EXPECT_EXIT=${VERIFY_EXPECT_EXIT}" \
  "${NODE_CONTAINER_NAME}" \
  /bin/bash /workspace/scripts/verify/verify-node-image-e2e-in-container.sh; then
  dump_logs
  exit 1
fi

echo "verify_node_${E2E_KIND}_e2e_host_ok=true"
