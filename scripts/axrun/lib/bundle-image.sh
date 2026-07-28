#!/usr/bin/env bash

axrun_bundle_import_archive=""
axrun_bundle_import_remote_archive=""
axrun_bundle_import_node_container=""

axrun_cleanup_bundle_import_artifacts() {
  if [ -n "${axrun_bundle_import_archive}" ]; then
    rm -f "${axrun_bundle_import_archive}"
  fi
  if [ -n "${axrun_bundle_import_remote_archive}" ] && [ -n "${axrun_bundle_import_node_container}" ]; then
    docker exec "${axrun_bundle_import_node_container}" rm -f "${axrun_bundle_import_remote_archive}" >/dev/null 2>&1 || true
  fi
}

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
  local archive
  local remote_archive
  local digest
  local imported_ref

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

  archive="$(mktemp -t axrun-bundle-image.XXXXXX.tar)"
  remote_archive="/tmp/axrun-bundle-image-$(date +%s%N).tar"
  axrun_bundle_import_archive="${archive}"
  axrun_bundle_import_remote_archive="${remote_archive}"
  axrun_bundle_import_node_container="${node_container}"

  docker save -o "${archive}" "${local_image}" >/dev/null
  digest="sha256:$(shasum -a 256 "${archive}" | awk '{print $1}')"
  imported_ref="$(axrun_normalize_digest_repository "${repository}")@${digest}"
  docker cp "${archive}" "${node_container}:${remote_archive}" >/dev/null
  docker exec "${node_container}" axctl image import \
    --imagemgr-socket /run/imagemgr/imagemgr.sock \
    --archive "${remote_archive}" \
    --ref "${imported_ref}" >/dev/null
  axrun_cleanup_bundle_import_artifacts
  axrun_bundle_import_archive=""
  axrun_bundle_import_remote_archive=""
  axrun_bundle_import_node_container=""
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
