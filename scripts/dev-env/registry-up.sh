#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd curl

if ! docker inspect "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1; then
  docker run -d --restart=always \
    -p "127.0.0.1:${LOCAL_REGISTRY_PORT}:5000" \
    --name "${LOCAL_REGISTRY_NAME}" \
    "${LOCAL_REGISTRY_IMAGE}" >/dev/null
elif ! registry_container_running; then
  docker start "${LOCAL_REGISTRY_NAME}" >/dev/null
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
