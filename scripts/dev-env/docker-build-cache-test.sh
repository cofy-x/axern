#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/dev-env/docker-build-cache.sh"

calls_file="$(mktemp "${TMPDIR:-/tmp}/axern-docker-build-cache.XXXXXX")"
trap 'rm -f -- "${calls_file}"' EXIT

docker() {
  if [ "${1:-} ${2:-}" = "buildx version" ]; then
    return "${MOCK_BUILDX_VERSION_STATUS:-0}"
  fi
  printf '%s\n' "$*" >> "${calls_file}"
}

assert_contains() {
  local expected="$1"
  if ! grep -Fq -- "${expected}" "${calls_file}"; then
    echo "expected Docker call fragment not found: ${expected}" >&2
    return 1
  fi
}

: > "${calls_file}"
AXERN_DOCKER_CACHE_BACKEND=none \
  AXERN_OCI_SOURCE_LABEL=https://github.com/cofy-x/axern \
  axern_docker_build -t example.test/axern/node-runtime-base:v1 .
assert_contains "build -t example.test/axern/node-runtime-base:v1 . --label org.opencontainers.image.source=https://github.com/cofy-x/axern"

if output="$(AXERN_DOCKER_CACHE_BACKEND=gha MOCK_BUILDX_VERSION_STATUS=1 \
  axern_docker_build -t example.test/axern/node-runtime-base:v1 . 2>&1)"; then
  echo "GHA cache unexpectedly accepted a missing Buildx runtime" >&2
  exit 1
fi
grep -Fq "Docker Buildx is required" <<< "${output}"

if output="$(AXERN_DOCKER_CACHE_BACKEND=gha \
  ACTIONS_RUNTIME_TOKEN='' ACTIONS_RESULTS_URL='' ACTIONS_CACHE_URL='' \
  axern_docker_build -t example.test/axern/node-runtime-base:v1 . 2>&1)"; then
  echo "GHA cache unexpectedly accepted a missing Actions runtime" >&2
  exit 1
fi
grep -Fq "ACTIONS_RUNTIME_TOKEN is required" <<< "${output}"

: > "${calls_file}"
output="$(
  AXERN_DOCKER_CACHE_BACKEND=gha \
  AXERN_TARGET_GOARCH=arm64 \
  ACTIONS_RUNTIME_TOKEN=test-runtime-token \
  ACTIONS_RESULTS_URL=https://results.example.test/ \
    axern_docker_build -t example.test/axern/node-runtime-base:v1 .
)"
grep -Fq "docker_build_cache_backend=gha scope=axern-node-runtime-base-arm64" <<< "${output}"
assert_contains "buildx build --load --cache-from type=gha,scope=axern-node-runtime-base-arm64,timeout=20m"
assert_contains "--cache-to type=gha,scope=axern-node-runtime-base-arm64,mode=max,timeout=20m"
if grep -Fq "test-runtime-token" "${calls_file}"; then
  echo "GHA runtime token leaked into Docker arguments" >&2
  exit 1
fi

if output="$(AXERN_DOCKER_CACHE_BACKEND=registry \
  axern_docker_build -t example.test/axern/node-runtime-base:v1 . 2>&1)"; then
  echo "unsupported cache backend unexpectedly succeeded" >&2
  exit 1
fi
grep -Fq "AXERN_DOCKER_CACHE_BACKEND must be none or gha" <<< "${output}"

echo "docker_build_cache_test_ok=true"
