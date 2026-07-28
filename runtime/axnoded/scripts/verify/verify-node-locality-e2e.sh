#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
AXNODED_HTTP_ADDRESS="${AXNODED_HTTP_ADDRESS:-0.0.0.0:23001}"
AXNODED_HTTP_URL="${AXNODED_HTTP_URL:-http://127.0.0.1:${AXNODED_HTTP_ADDRESS##*:}}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axnoded-node-locality-e2e}"
CREATE_SANDBOX_TIMEOUT="${CREATE_SANDBOX_TIMEOUT:-300s}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
case "${VERIFY_DOCKER_PLATFORM}" in
  linux/amd64)
    default_oci_image="docker.io/library/busybox@sha256:b8d1827e38a1d49cd17217efd7b07d689e4ea1744e39c7dcbb95533d175bea65"
    default_nydus_image="ghcr.io/dragonflyoss/image-service/nginx@sha256:e263899b73cfecb68980fbe396dbac8dbabd108397786bed8b423c496500a2a7"
    ;;
  linux/arm64 | linux/arm64/v8)
    default_oci_image="docker.io/library/busybox@sha256:c4e5b27bf840ba1ebd5568b6b914f6926f3559b2ad4f505b1f37aae483b907d6"
    default_nydus_image="ghcr.io/dragonflyoss/image-service/nginx@sha256:02cde82e5688297fdc6e011b4a4a5535ee106a3d39ecdef8005b45244e39ede2"
    ;;
  *)
    echo "unsupported VERIFY_DOCKER_PLATFORM for locality e2e: ${VERIFY_DOCKER_PLATFORM}" >&2
    exit 1
    ;;
esac

OCI_IMAGE_URL="${OCI_TEST_IMAGE:-${default_oci_image}}"
if [ -n "${NYDUS_TEST_IMAGE:-}" ]; then
  NYDUS_IMAGE_URL="${NYDUS_TEST_IMAGE}"
  NYDUS_TEST_IMAGE_SOURCE="${NYDUS_TEST_IMAGE_SOURCE:-registry}"
else
  NYDUS_IMAGE_URL="${default_nydus_image}"
fi

prepare_oci_test_image_source "${OCI_IMAGE_URL}"
OCI_IMAGE_URL="${PREPARED_OCI_TEST_IMAGE}"
prepare_nydus_test_image_source "${NYDUS_IMAGE_URL}"
NYDUS_IMAGE_URL="${PREPARED_NYDUS_TEST_IMAGE}"

export VERIFY_DOCKER_PLATFORM

dump_logs() {
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 200 /var/log/axnoded/axnoded.log >&2 || true
  echo "--- imagemgr log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 200 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
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
  "${IMAGE_TAG}" \
  /bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >/dev/null

deadline=$((SECONDS + 180))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if ! docker inspect -f '{{.State.Running}}' "${NODE_CONTAINER_NAME}" 2>/dev/null | grep -qx true; then
    echo "node container exited before becoming ready" >&2
    dump_logs
    exit 1
  fi
  if docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${IMAGEMGR_SOCKET}' && test -S '${AXNODED_SOCKET}' && curl -fsS '${AXNODED_HTTP_URL}/readyz' >/dev/null"; then
    break
  fi
  sleep 2
done

if ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "test -S '${IMAGEMGR_SOCKET}' && test -S '${AXNODED_SOCKET}' && curl -fsS '${AXNODED_HTTP_URL}/readyz' >/dev/null"; then
  echo "node container did not become ready in time" >&2
  dump_logs
  exit 1
fi

if ! docker exec \
  -e "IMAGEMGR_SOCKET=${IMAGEMGR_SOCKET}" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "AXNODED_HTTP_URL=${AXNODED_HTTP_URL}" \
  -e "CREATE_SANDBOX_TIMEOUT=${CREATE_SANDBOX_TIMEOUT}" \
  -e "OCI_IMAGE_URL=${OCI_IMAGE_URL}" \
  -e "NYDUS_IMAGE_URL=${NYDUS_IMAGE_URL}" \
  "${NODE_CONTAINER_NAME}" \
  /bin/bash /workspace/scripts/verify/verify-node-locality-e2e-in-container.sh; then
  dump_logs
  exit 1
fi

echo "verify_node_locality_e2e_host_ok=true"
