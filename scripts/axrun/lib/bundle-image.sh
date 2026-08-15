#!/usr/bin/env bash

axrun_normalize_digest_repository() {
  local repository="$1"
  if [[ "${repository}" != */* ]]; then
    printf 'index.docker.io/library/%s\n' "${repository}"
    return 0
  fi
  local first="${repository%%/*}"
  if [[ "${first}" == *.* || "${first}" == *:* || "${first}" == "localhost" ]]; then
    printf '%s\n' "${repository}"
    return 0
  fi
  printf 'index.docker.io/%s\n' "${repository}"
}

axrun_import_bundle_image_to_compose() {
  local local_image="$1"
  local repository="$2"
  local project="${COMPOSE_PROJECT_NAME:-axern-local}"
  local node_container="${project}-node-1"
  local digest
  local imported_ref
  local import_ref
  local import_result
  local image_id

  if ! docker image inspect "${local_image}" >/dev/null 2>&1; then
    echo "missing bundle image ${local_image}; build it before running this smoke" >&2
    exit 1
  fi
  if [[ -z "${repository}" || "${repository}" == *@* ]]; then
    echo "invalid agent import repository ${repository}; use an image repository without digest" >&2
    exit 1
  fi
  if [[ "${repository##*/}" == *:* ]]; then
    echo "invalid agent import repository ${repository}; use an image repository without tag" >&2
    exit 1
  fi

  image_id="$(docker image inspect "${local_image}" --format '{{.Id}}')"
  import_ref="$(axrun_normalize_digest_repository "${repository}"):local-import"
  import_result="$(docker image save "${image_id}" | docker exec -i "${node_container}" axctl image import \
    --imagemgr-socket /run/imagemgr/imagemgr.sock \
    --file - \
    --ref "${import_ref}" \
    --json)"
  digest="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["generation_digest"])' <<<"${import_result}")"
  imported_ref="$(axrun_normalize_digest_repository "${repository}")@${digest}"
  printf '%s\n' "${imported_ref}"
}

axrun_push_bundle_image_to_compose_registry() {
  local local_image="$1"
  local output
  local cluster_image

  output="$(IMAGE="${local_image}" bash "${AXERN_ROOT}/scripts/dev-env/registry-image-push.sh")"
  cluster_image="$(printf '%s\n' "${output}" | awk -F= '$1 == "cluster_image" {print $2}')"
  if [ -z "${cluster_image}" ]; then
    printf '%s\n' "${output}" >&2
    echo "bundle image registry push did not return cluster_image" >&2
    exit 1
  fi
  printf '%s\n' "${cluster_image}"
}
