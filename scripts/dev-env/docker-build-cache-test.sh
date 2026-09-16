#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/dev-env/docker-build-cache.sh"

calls_file="$(mktemp "${TMPDIR:-/tmp}/axern-docker-build-cache.XXXXXX")"
session_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-cache-test.XXXXXX")"
trap 'rm -f -- "${calls_file}"; rm -rf -- "${session_dir}"' EXIT

docker() {
  if [ "${1:-} ${2:-}" = "buildx version" ]; then
    return "${MOCK_BUILDX_VERSION_STATUS:-0}"
  fi
  if [ "${1:-} ${2:-}" = "image inspect" ] && [ "${MOCK_IMAGE_INSPECT_STATUS:-0}" != "0" ]; then
    return "${MOCK_IMAGE_INSPECT_STATUS}"
  fi
  if [ "${1:-} ${2:-}" = "image inspect" ] && [ "${3:-}" = --format ]; then
    printf 'sha256:%064d\n' "${MOCK_IMAGE_NUMBER:-1}"
    return 0
  fi
  printf '%s\n' "$*" >> "${calls_file}"
  if [[ "$*" == *--cache-to* ]]; then
    return "${MOCK_EXPORT_STATUS:-0}"
  fi
  if [ "${1:-} ${2:-} ${3:-}" = 'buildx build --load' ]; then
    return "${MOCK_BUILD_STATUS:-0}"
  fi
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

session_build() {
  AXERN_DOCKER_CACHE_BACKEND=gha \
    AXERN_DOCKER_CACHE_SESSION_DIR="${session_dir}" \
    ACTIONS_RUNTIME_TOKEN=test-runtime-token \
    ACTIONS_RESULTS_URL=https://results.example.test/ \
    axern_docker_build -t example.test/axern/session:v1 .
}
: >"${calls_file}"
session_build
session_build
[ "$(grep -c -- 'buildx build --load' "${calls_file}")" = 2 ]
[ "$(grep -c -- '--cache-to' "${calls_file}")" = 1 ]
MOCK_IMAGE_NUMBER=2 session_build
[ "$(grep -c -- '--cache-to' "${calls_file}")" = 2 ]
if MOCK_IMAGE_NUMBER=3 MOCK_EXPORT_STATUS=7 session_build; then
  echo "failed cache export unexpectedly succeeded" >&2
  exit 1
fi
MOCK_IMAGE_NUMBER=3 session_build
[ "$(grep -c -- '--cache-to' "${calls_file}")" = 4 ]
assert_contains 'buildx build --output=type=cacheonly --cache-to'
if MOCK_BUILD_STATUS=9 session_build; then
  echo "failed build unexpectedly reused an export receipt" >&2
  exit 1
fi
if MOCK_IMAGE_INSPECT_STATUS=1 session_build; then
  echo "missing built image unexpectedly reused an export receipt" >&2
  exit 1
fi
[ "$(grep -c -- '--cache-to' "${calls_file}")" = 4 ]

if output="$(AXERN_DOCKER_CACHE_BACKEND=registry \
  axern_docker_build -t example.test/axern/node-runtime-base:v1 . 2>&1)"; then
  echo "unsupported cache backend unexpectedly succeeded" >&2
  exit 1
fi
grep -Fq "AXERN_DOCKER_CACHE_BACKEND must be none or gha" <<< "${output}"

: > "${calls_file}"
source "${AXERN_ROOT}/runtime/axnoded/scripts/lib/verify-docker-common.sh"
VERIFY_DOCKER_VARIANT=full
VERIFY_DOCKER_PLATFORM=linux/amd64
NODE_RUNTIME_BASE_IMAGE_TAG=axern/local-node-runtime-base:dev
build_verify_image archive crates-io
assert_contains "image inspect axern/local-node-runtime-base:dev"
assert_contains "build --platform linux/amd64 -f"
if grep -Fq "buildx build" "${calls_file}"; then
  echo "local-parent build unexpectedly used an isolated Buildx image store" >&2
  exit 1
fi

if output="$(MOCK_IMAGE_INSPECT_STATUS=1 \
  axern_docker_build_from_local_parent missing-parent:dev -t child:dev . 2>&1)"; then
  echo "local-parent build unexpectedly accepted a missing parent" >&2
  exit 1
fi
grep -Fq "required local parent image is not loaded: missing-parent:dev" <<< "${output}"

echo "docker_build_cache_test_ok=true"
