#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd curl

begin_named_lock "registry-nydus-image-build"
trap 'end_named_lock "registry-nydus-image-build"' EXIT

source_image="${NYDUS_SOURCE_IMAGE}"
source_registry_image="${NYDUS_SOURCE_REGISTRY_IMAGE}"
target_image="${NYDUS_LOCAL_IMAGE}"
target_runtime_image="${target_image/#${LOCAL_REGISTRY_HOST}/${LOCAL_REGISTRY_CLUSTER_HOST}}"
source_runtime_image="${source_registry_image/#${LOCAL_REGISTRY_HOST}/${LOCAL_REGISTRY_CLUSTER_HOST}}"
platform="${NYDUS_PLATFORM:-}"

registry_manifest_exists() {
  local image_ref="$1"
  local repo_tag repo tag
  if [[ "${image_ref}" != "${LOCAL_REGISTRY_HOST}/"* ]]; then
    return 1
  fi
  repo_tag="${image_ref#${LOCAL_REGISTRY_HOST}/}"
  if [[ "${repo_tag}" == *@* ]] || [[ "${repo_tag}" != *:* ]]; then
    return 1
  fi
  repo="${repo_tag%:*}"
  tag="${repo_tag##*:}"
  curl -fsS \
    -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
    "http://127.0.0.1:${LOCAL_REGISTRY_PORT}/v2/${repo}/manifests/${tag}" \
    >/dev/null
}

if [[ "${source_registry_image}" != "${LOCAL_REGISTRY_HOST}/"* ]]; then
  echo "NYDUS_SOURCE_REGISTRY_IMAGE must use ${LOCAL_REGISTRY_HOST}, got: ${source_registry_image}" >&2
  exit 2
fi
if [[ "${target_image}" != "${LOCAL_REGISTRY_HOST}/"* ]]; then
  echo "NYDUS_LOCAL_IMAGE must use ${LOCAL_REGISTRY_HOST}, got: ${target_image}" >&2
  exit 2
fi

bash "${AXERN_ROOT}/scripts/dev-env/registry-up.sh" >/dev/null

if [ -z "${platform}" ]; then
  docker_os="$(docker version --format '{{.Server.Os}}' 2>/dev/null || true)"
  docker_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)"
  docker_os="${docker_os:-linux}"
  case "${docker_arch}" in
    aarch64) docker_arch="arm64" ;;
    x86_64) docker_arch="amd64" ;;
  esac
  platform="${docker_os}/${docker_arch}"
fi

if [ "${NYDUS_IMAGE_REBUILD:-}" != "1" ] && registry_manifest_exists "${target_image}"; then
  echo "registry_nydus_image_build_ok=true"
  echo "nydus_image_reused=true"
  echo "source_image=${source_image}"
  echo "source_registry_image=${source_registry_image}"
  echo "nydus_image=${target_image}"
  echo "cluster_nydus_image=${target_runtime_image}"
  echo "nydus_platform=${platform}"
  exit 0
fi

if ! docker image inspect "${source_image}" >/dev/null 2>&1; then
  bash "${AXERN_ROOT}/scripts/dev-env/build-images.sh" >/dev/null
fi
if ! docker image inspect "${source_image}" >/dev/null 2>&1; then
  echo "missing Nydus source image after local image build: ${source_image}" >&2
  exit 1
fi

if ! docker image inspect "${NYDUS_BUILDER_IMAGE}" >/dev/null 2>&1; then
  bash "${AXERN_ROOT}/scripts/dev-env/build-nydus-builder-image.sh" >/dev/null
fi

IMAGE="${source_image}" TARGET="${source_registry_image}" \
  bash "${AXERN_ROOT}/scripts/dev-env/registry-image-push.sh" >/dev/null

docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  "${NYDUS_BUILDER_IMAGE}" \
  convert \
  --nydus-image /usr/local/bin/nydus-image \
  --plain-http \
  --source-insecure \
  --target-insecure \
  --platform "${platform}" \
  --source "${source_runtime_image}" \
  --target "${target_runtime_image}"

if ! registry_manifest_exists "${target_image}"; then
  echo "Nydus conversion completed but ${target_image} is not inspectable from the host registry" >&2
  exit 1
fi

echo "registry_nydus_image_build_ok=true"
echo "nydus_image_reused=false"
echo "source_image=${source_image}"
echo "source_registry_image=${source_registry_image}"
echo "nydus_image=${target_image}"
echo "cluster_nydus_image=${target_runtime_image}"
echo "nydus_platform=${platform}"
