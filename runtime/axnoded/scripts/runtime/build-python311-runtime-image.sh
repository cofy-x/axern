#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/scripts/dev-env/docker-build-cache.sh"
IMAGE_REF="${IMAGE_REF:-axern/python311-runtime:dev}"
DOCKERFILE="${ROOT_DIR}/docker/runtimes/python311/Dockerfile"
CONTEXT_DIR="${REPO_ROOT}"

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
  --build-arg "PIP_INDEX_URL=${PIP_INDEX_URL:-}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_REF}" \
  "${CONTEXT_DIR}"
