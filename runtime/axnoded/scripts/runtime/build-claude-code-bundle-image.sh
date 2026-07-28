#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/scripts/dev-env/docker-build-cache.sh"

IMAGE_REF="${IMAGE_REF:-axern/claude-code-bundle:dev}"
CLAUDE_CODE_VERSION="${CLAUDE_CODE_VERSION:-2.1.205}"
DOCKERFILE="${ROOT_DIR}/docker/runtimes/claude-code-bundle/Dockerfile"
CONTEXT_DIR="$(dirname "${DOCKERFILE}")"

docker_proxy_url() {
  printf '%s\n' "${1:-}" | sed -E 's#(https?://)(127\.0\.0\.1|localhost)(:[0-9]+)?#\1host.docker.internal\3#g'
}

HTTP_PROXY_BUILD="$(docker_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
HTTPS_PROXY_BUILD="$(docker_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
NO_PROXY_BUILD="${NO_PROXY:-${no_proxy:-}}"

axern_docker_build \
	--build-arg "HTTP_PROXY=${HTTP_PROXY_BUILD}" \
	--build-arg "HTTPS_PROXY=${HTTPS_PROXY_BUILD}" \
	--build-arg "NO_PROXY=${NO_PROXY_BUILD}" \
	--build-arg "CLAUDE_CODE_VERSION=${CLAUDE_CODE_VERSION}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_REF}" \
  "${CONTEXT_DIR}"
