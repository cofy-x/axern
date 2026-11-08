#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd curl

if ! docker image inspect "${LOCAL_REGISTRY_IMAGE}" >/dev/null 2>&1; then
  if [ "${LOCAL_REGISTRY_IMAGE}" = axern/local-registry:dev ]; then
    AXERN_LOCAL_IMAGE_SCOPE=dependencies bash "${AXERN_ROOT}/scripts/dev-env/build-images.sh"
  else
    ensure_host_image "${LOCAL_REGISTRY_IMAGE}"
  fi
fi

if ! docker inspect "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1; then
  docker run -d --restart=always \
    -p "127.0.0.1:${LOCAL_REGISTRY_PORT}:5000" \
    --name "${LOCAL_REGISTRY_NAME}" \
    "${LOCAL_REGISTRY_IMAGE}" >/dev/null
else
  expected_image="$(docker image inspect "${LOCAL_REGISTRY_IMAGE}" --format '{{.Id}}')"
  current_image="$(docker inspect "${LOCAL_REGISTRY_NAME}" --format '{{.Image}}')"
  if [ "${current_image}" != "${expected_image}" ]; then
    echo "registry ${LOCAL_REGISTRY_NAME} uses a different image; preserve its /var/lib/registry volume and recreate it with ${LOCAL_REGISTRY_IMAGE} before continuing" >&2
    exit 1
  fi
  if ! registry_container_running; then
    docker start "${LOCAL_REGISTRY_NAME}" >/dev/null
  fi
fi

deadline=$((SECONDS + 30))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if registry_http_ready; then
    break
  fi
  sleep 1
done
if ! registry_http_ready; then
  echo "local registry did not become ready at 127.0.0.1:${LOCAL_REGISTRY_PORT}" >&2
  exit 1
fi

connect_registry_to_kind_network || true

echo "registry_up_ok=true"
emit_registry_status
