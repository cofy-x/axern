#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
. "${ROOT_DIR}/scripts/verify/docker-env.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(default_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM
SANDBOXD_E2E_IMAGE="${SANDBOXD_E2E_IMAGE:-${GO_IMAGE:-golang:1.25.12}}"
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
GOSUMDB="${GOSUMDB:-sum.golang.org}"
VERIFY_SANDBOXD_PROVIDER_SCOPE="${VERIFY_SANDBOXD_PROVIDER_SCOPE:-full}"

case "${VERIFY_SANDBOXD_PROVIDER_SCOPE}" in
  contract|full) ;;
  *)
    echo "unsupported VERIFY_SANDBOXD_PROVIDER_SCOPE=${VERIFY_SANDBOXD_PROVIDER_SCOPE}; want contract or full" >&2
    exit 1
    ;;
esac

docker_proxy_url() {
  local raw="$1"
  if [ -z "${raw}" ]; then
    printf '%s\n' ""
    return 0
  fi
  printf '%s\n' "${raw}" | sed -E 's#://(localhost|127\.0\.0\.1|\[::1\])([:/]|$)#://host.docker.internal\2#g'
}

run_args=(
  --rm
  --platform "${VERIFY_DOCKER_PLATFORM}"
  -v "${REPO_ROOT}:/workspace:ro"
  -w /workspace/runtime/axnoded
  -e "GOFLAGS=${GOFLAGS:--mod=mod}"
  -e "GOWORK=off"
  -e "GOPROXY=${GOPROXY}"
  -e "GOSUMDB=${GOSUMDB}"
)

if [ -n "${VERIFY_DOCKER_HTTP_PROXY:-${HTTP_PROXY:-${http_proxy:-}}}" ]; then
  proxy="$(docker_proxy_url "${VERIFY_DOCKER_HTTP_PROXY:-${HTTP_PROXY:-${http_proxy:-}}}")"
  run_args+=(-e "HTTP_PROXY=${proxy}" -e "http_proxy=${proxy}")
fi
if [ -n "${VERIFY_DOCKER_HTTPS_PROXY:-${HTTPS_PROXY:-${https_proxy:-}}}" ]; then
  proxy="$(docker_proxy_url "${VERIFY_DOCKER_HTTPS_PROXY:-${HTTPS_PROXY:-${https_proxy:-}}}")"
  run_args+=(-e "HTTPS_PROXY=${proxy}" -e "https_proxy=${proxy}")
fi
if [ -n "${VERIFY_DOCKER_NO_PROXY:-${NO_PROXY:-${no_proxy:-}}}" ]; then
  no_proxy_value="${VERIFY_DOCKER_NO_PROXY:-${NO_PROXY:-${no_proxy:-}}},host.docker.internal"
  run_args+=(-e "NO_PROXY=${no_proxy_value}" -e "no_proxy=${no_proxy_value}")
fi

docker run "${run_args[@]}" \
  "${SANDBOXD_E2E_IMAGE}" \
  /bin/bash /workspace/runtime/axnoded/scripts/verify/verify-sandboxd-provider-e2e-in-container.sh

if [ "${VERIFY_SANDBOXD_PROVIDER_SCOPE}" = "contract" ]; then
  echo "verify_sandboxd_provider_smoke_ok=true"
  exit 0
fi

bash "${ROOT_DIR}/scripts/verify/verify-sandboxd-e2e.sh"
bash "${ROOT_DIR}/scripts/verify/verify-sandboxd-computer-use-e2e.sh"
bash "${ROOT_DIR}/scripts/verify/verify-sandboxd-oci-e2e.sh"

echo "verify_sandboxd_provider_e2e_ok=true"
