#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/scripts/dev-env/docker-build-cache.sh"
IMAGE_REF="${IMAGE_REF:-axern/server-base-runtime:dev}"
DOCKERFILE="${ROOT_DIR}/docker/runtimes/server-base/Dockerfile"
CONTEXT_DIR="$(dirname "${DOCKERFILE}")"
APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-upstream}"
if [ "${APT_MIRROR_SOURCE}" = "archive" ]; then
  APT_MIRROR_SOURCE="upstream"
fi

docker_proxy_url() {
  local raw="${1:-}"
  if [ -z "${raw}" ]; then
    return 0
  fi
  printf '%s\n' "${raw}" | sed -E 's#(https?://)(127\.0\.0\.1|localhost)(:[0-9]+)?#\1host.docker.internal\3#g'
}

HTTP_PROXY_BUILD="$(docker_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
HTTPS_PROXY_BUILD="$(docker_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
NO_PROXY_BUILD="${NO_PROXY:-${no_proxy:-}}"

axern_docker_build \
  --build-arg "HTTP_PROXY=${HTTP_PROXY_BUILD}" \
  --build-arg "HTTPS_PROXY=${HTTPS_PROXY_BUILD}" \
  --build-arg "NO_PROXY=${NO_PROXY_BUILD}" \
  --build-arg "http_proxy=${HTTP_PROXY_BUILD}" \
  --build-arg "https_proxy=${HTTPS_PROXY_BUILD}" \
  --build-arg "no_proxy=${NO_PROXY_BUILD}" \
  --build-arg "APT_MIRROR_SOURCE=${APT_MIRROR_SOURCE}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_REF}" \
  "${CONTEXT_DIR}"
