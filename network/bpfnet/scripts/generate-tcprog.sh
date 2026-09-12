#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-generate}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
# shellcheck source=../../../scripts/proxy-env.sh
source "${REPO_ROOT}/scripts/proxy-env.sh"

IMAGE_TAG="${BPFNET_CODEGEN_IMAGE:-axern-bpfnet-codegen:latest}"
DOCKER_PLATFORM="${BPFNET_CODEGEN_PLATFORM:-}"
CACHE_ROOT="${BPFNET_CODEGEN_CACHE_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/axern/bpfnet-codegen}"
GOPROXY_VALUE="${BPFNET_CODEGEN_GOPROXY:-https://proxy.golang.org,direct}"
GOSUMDB_VALUE="${BPFNET_CODEGEN_GOSUMDB:-sum.golang.org}"
APT_MIRROR_BASE_URL_VALUE="${BPFNET_CODEGEN_APT_MIRROR_BASE_URL:-${APT_MIRROR_BASE_URL:-}}"
HTTP_PROXY_VALUE="$(container_proxy_url "${BPFNET_CODEGEN_HTTP_PROXY:-${HTTP_PROXY:-${http_proxy:-}}}")"
HTTPS_PROXY_VALUE="$(container_proxy_url "${BPFNET_CODEGEN_HTTPS_PROXY:-${HTTPS_PROXY:-${https_proxy:-}}}")"
NO_PROXY_VALUE="$(append_no_proxy_entries \
  "${BPFNET_CODEGEN_NO_PROXY:-${NO_PROXY:-${no_proxy:-}}}" \
  "localhost,127.0.0.1,::1,host.docker.internal")"

build_args=(
  --add-host "host.docker.internal:host-gateway"
  --build-arg "APT_MIRROR_BASE_URL=${APT_MIRROR_BASE_URL_VALUE}"
  --build-arg "GOPROXY=${GOPROXY_VALUE}"
  --build-arg "GOSUMDB=${GOSUMDB_VALUE}"
  -t "${IMAGE_TAG}"
  -f "${ROOT_DIR}/docker/codegen/Dockerfile"
  "${ROOT_DIR}/docker/codegen"
)
if [ -n "${HTTP_PROXY_VALUE}" ]; then
  build_args+=(
    --build-arg "HTTP_PROXY=${HTTP_PROXY_VALUE}"
    --build-arg "http_proxy=${HTTP_PROXY_VALUE}"
  )
fi
if [ -n "${HTTPS_PROXY_VALUE}" ]; then
  build_args+=(
    --build-arg "HTTPS_PROXY=${HTTPS_PROXY_VALUE}"
    --build-arg "https_proxy=${HTTPS_PROXY_VALUE}"
  )
fi
if [ -n "${NO_PROXY_VALUE}" ]; then
  build_args+=(
    --build-arg "NO_PROXY=${NO_PROXY_VALUE}"
    --build-arg "no_proxy=${NO_PROXY_VALUE}"
  )
fi
if [ -n "${DOCKER_PLATFORM}" ]; then
  build_args=(--platform "${DOCKER_PLATFORM}" "${build_args[@]}")
fi

docker build "${build_args[@]}" >/dev/null
mkdir -p "${CACHE_ROOT}"

run_args=(
  run --rm
  --add-host "host.docker.internal:host-gateway"
  --user "$(id -u):$(id -g)"
  -e HOME=/tmp/bpfnet-codegen-home
  -e GOPATH=/tmp/bpfnet-codegen-cache/go
  -e GOMODCACHE=/tmp/bpfnet-codegen-cache/gomod
  -e GOCACHE=/tmp/bpfnet-codegen-cache/go-build
  -e GOWORK=off
  -e GOPROXY="${GOPROXY_VALUE}"
  -e GOSUMDB="${GOSUMDB_VALUE}"
  -e PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  -v "${CACHE_ROOT}:/tmp/bpfnet-codegen-cache"
  -v "${REPO_ROOT}:/workspace"
  -w /workspace/network/bpfnet
)
if [ -n "${HTTP_PROXY_VALUE}" ]; then
  run_args+=(
    -e "HTTP_PROXY=${HTTP_PROXY_VALUE}"
    -e "http_proxy=${HTTP_PROXY_VALUE}"
  )
fi
if [ -n "${HTTPS_PROXY_VALUE}" ]; then
  run_args+=(
    -e "HTTPS_PROXY=${HTTPS_PROXY_VALUE}"
    -e "https_proxy=${HTTPS_PROXY_VALUE}"
  )
fi
if [ -n "${NO_PROXY_VALUE}" ]; then
  run_args+=(
    -e "NO_PROXY=${NO_PROXY_VALUE}"
    -e "no_proxy=${NO_PROXY_VALUE}"
  )
fi
run_args+=("${IMAGE_TAG}")

case "${MODE}" in
  generate)
    docker "${run_args[@]}" bash -c 'mkdir -p "${HOME}" && /usr/bin/git config --global --add safe.directory /workspace && /usr/local/go/bin/go generate ./internal/tcprog'
    ;;
  check)
    docker "${run_args[@]}" bash -c 'mkdir -p "${HOME}" && /usr/bin/git config --global --add safe.directory /workspace && /usr/local/go/bin/go generate ./internal/tcprog && /usr/bin/git -C /workspace diff --exit-code -- network/bpfnet/internal/tcprog/dataplane_bpfel.go network/bpfnet/internal/tcprog/dataplane_bpfeb.go network/bpfnet/internal/tcprog/dataplane_bpfel.o network/bpfnet/internal/tcprog/dataplane_bpfeb.o'
    ;;
  *)
    echo "usage: $0 [generate|check]" >&2
    exit 1
    ;;
esac
