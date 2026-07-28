#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
AXNODED_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
REPO_ROOT=$(git -C "${AXNODED_DIR}" rev-parse --show-toplevel)

PROTO_IMAGE_TAG=${PROTO_IMAGE_TAG:-axnoded-proto-tools:latest}
BUILD_ONLY=0

if [[ "${1:-}" == "--build-only" ]]; then
  BUILD_ONLY=1
fi

docker build \
  -t "${PROTO_IMAGE_TAG}" \
  -f "${AXNODED_DIR}/docker/proto/Dockerfile" \
  "${REPO_ROOT}"

if [[ "${BUILD_ONLY}" == "1" ]]; then
  exit 0
fi

docker run --rm \
  -v "${REPO_ROOT}:/workspace" \
  -w /workspace \
  "${PROTO_IMAGE_TAG}" \
  bash -c 'make -C runtime/axnoded protos'
