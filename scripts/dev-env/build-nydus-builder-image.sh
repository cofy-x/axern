#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
source "${AXERN_ROOT}/scripts/dev-env/docker-build-cache.sh"

require_cmd docker
apt_mirror_source="${APT_MIRROR_SOURCE:-archive}"

build_http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
build_https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
build_no_proxy="$(append_no_proxy_entries "${NO_PROXY:-${no_proxy:-}}" "localhost,127.0.0.1,::1,host.docker.internal")"
build_args=()
if [ -n "${build_http_proxy}" ]; then
  build_args+=(
    --build-arg "HTTP_PROXY=${build_http_proxy}"
    --build-arg "http_proxy=${build_http_proxy}"
  )
fi
if [ -n "${build_https_proxy}" ]; then
  build_args+=(
    --build-arg "HTTPS_PROXY=${build_https_proxy}"
    --build-arg "https_proxy=${build_https_proxy}"
  )
fi
if [ -n "${build_no_proxy}" ]; then
  build_args+=(
    --build-arg "NO_PROXY=${build_no_proxy}"
    --build-arg "no_proxy=${build_no_proxy}"
  )
fi

axern_docker_build \
  -f "${AXERN_ROOT}/deploy/images/nydus-builder/Dockerfile" \
  "${build_args[@]}" \
  --build-arg "APT_MIRROR_SOURCE=${apt_mirror_source}" \
  --build-arg "NYDUS_VERSION=${NYDUS_BUILDER_NYDUS_VERSION:-v2.4.0}" \
  --build-arg "BUILDKIT_VERSION=${NYDUS_BUILDER_BUILDKIT_VERSION:-v0.24.0}" \
  -t "${NYDUS_BUILDER_IMAGE}" \
  "${AXERN_ROOT}"

echo "nydus_builder_image_ready=true"
echo "nydus_builder_image=${NYDUS_BUILDER_IMAGE}"
