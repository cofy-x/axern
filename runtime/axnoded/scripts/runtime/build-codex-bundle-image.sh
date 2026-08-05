#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/scripts/dev-env/docker-build-cache.sh"

IMAGE_REF="${IMAGE_REF:-axern/codex-bundle:dev}"
CODEX_CLI_VERSION="${CODEX_CLI_VERSION:-0.144.6}"
DOCKERFILE="${ROOT_DIR}/docker/runtimes/codex-bundle/Dockerfile"
CONTEXT_DIR="$(dirname "${DOCKERFILE}")"
APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-upstream}"
if [ "${APT_MIRROR_SOURCE}" = "archive" ]; then
  APT_MIRROR_SOURCE="upstream"
fi
NODE_DOWNLOAD_BASE_URL="${NODE_DOWNLOAD_BASE_URL:-https://nodejs.org/dist}"
NPM_CONFIG_REGISTRY="${NPM_CONFIG_REGISTRY:-https://registry.npmjs.org}"

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
	--build-arg "http_proxy=${HTTP_PROXY_BUILD}" \
	--build-arg "https_proxy=${HTTPS_PROXY_BUILD}" \
	--build-arg "no_proxy=${NO_PROXY_BUILD}" \
	--build-arg "APT_MIRROR_SOURCE=${APT_MIRROR_SOURCE}" \
	--build-arg "NODE_DOWNLOAD_BASE_URL=${NODE_DOWNLOAD_BASE_URL}" \
	--build-arg "NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY}" \
	--build-arg "CODEX_CLI_VERSION=${CODEX_CLI_VERSION}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_REF}" \
  "${CONTEXT_DIR}"

bash "${ROOT_DIR}/scripts/runtime/verify-agent-bundle-image.sh" \
  "${IMAGE_REF}" codex codex "${CODEX_CLI_VERSION}" "codex-cli ${CODEX_CLI_VERSION}"
