#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-generate}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
IMAGE_TAG="${BPFNET_CODEGEN_IMAGE:-axern-bpfnet-codegen:latest}"
DOCKER_PLATFORM="${BPFNET_CODEGEN_PLATFORM:-}"
BPFNET_CODEGEN_REBUILD="${BPFNET_CODEGEN_REBUILD:-0}"
CACHE_ROOT="${BPFNET_CODEGEN_CACHE_DIR:-${XDG_CACHE_HOME:-${HOME}/.cache}/axern/bpfnet-codegen}"
GOPROXY_VALUE="${BPFNET_CODEGEN_GOPROXY:-https://proxy.golang.org,direct}"
GOSUMDB_VALUE="${BPFNET_CODEGEN_GOSUMDB:-sum.golang.org}"

build_args=(-t "${IMAGE_TAG}" -f "${ROOT_DIR}/docker/codegen/Dockerfile" "${ROOT_DIR}/docker/codegen")
if [ -n "${DOCKER_PLATFORM}" ]; then
  build_args=(--platform "${DOCKER_PLATFORM}" "${build_args[@]}")
fi

if [ "${BPFNET_CODEGEN_REBUILD}" = "1" ] || ! docker image inspect "${IMAGE_TAG}" >/dev/null 2>&1; then
  docker build "${build_args[@]}" >/dev/null
fi
mkdir -p "${CACHE_ROOT}"

run_args=(
  run --rm
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
  "${IMAGE_TAG}"
)

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
