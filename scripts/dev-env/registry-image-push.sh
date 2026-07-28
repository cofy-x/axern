#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker

image_ref="${IMAGE:-}"
if [ -z "${image_ref}" ]; then
  echo "IMAGE is required, for example: make registry-image-push IMAGE=axern/python311-runtime:dev" >&2
  exit 2
fi
if [[ "${image_ref}" == *@* ]]; then
  echo "IMAGE must be a tag reference that docker tag can retag, got: ${image_ref}" >&2
  exit 2
fi
if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
  echo "host Docker image not found: ${image_ref}" >&2
  exit 1
fi

bash "${AXERN_ROOT}/scripts/dev-env/registry-up.sh" >/dev/null

default_target_for_image() {
  local ref="$1"
  local first rest
  first="${ref%%/*}"
  if [[ "${ref}" == */* ]] && { [[ "${first}" == *.* ]] || [[ "${first}" == *:* ]] || [ "${first}" = "localhost" ]; }; then
    rest="${ref#*/}"
  else
    rest="${ref}"
  fi
  printf '%s/%s\n' "${LOCAL_REGISTRY_HOST}" "${rest}"
}

target_ref="${TARGET:-$(default_target_for_image "${image_ref}")}"
if [[ "${target_ref}" != "${LOCAL_REGISTRY_HOST}/"* ]]; then
  echo "TARGET must use ${LOCAL_REGISTRY_HOST}, got: ${target_ref}" >&2
  exit 2
fi
cluster_ref="${target_ref/#${LOCAL_REGISTRY_HOST}/${LOCAL_REGISTRY_CLUSTER_HOST}}"

docker tag "${image_ref}" "${target_ref}"
docker push "${target_ref}" >/dev/null

echo "registry_image_push_ok=true"
echo "pushed_image=${target_ref}"
echo "cluster_image=${cluster_ref}"
