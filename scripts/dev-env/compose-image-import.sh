#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker

image_ref="${IMAGE:-}"
if [ -z "${image_ref}" ]; then
  echo "IMAGE is required, for example: make local-compose-image-import IMAGE=myapp:dev" >&2
  exit 2
fi

if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
  echo "host Docker image not found: ${image_ref}" >&2
  exit 1
fi

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -Fxq "${node_container}"; then
  echo "compose node container is not running: ${node_container}" >&2
  exit 1
fi

import_timeout="${AXERN_IMAGE_IMPORT_TIMEOUT:-5m}"
image_id="$(docker image inspect "${image_ref}" --format '{{.Id}}')"

echo "Streaming ${image_ref} into compose node"
docker image save "${image_id}" | docker exec -i "${node_container}" axctl --timeout "${import_timeout}" image import \
  --imagemgr-socket /run/imagemgr/imagemgr.sock \
  --file - \
  --ref "${image_ref}"
