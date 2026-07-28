#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
AXNODED_DIR="${ROOT_DIR}/runtime/axnoded"

run_step() {
  local name="$1"
  shift
  echo "sandboxd_release_readiness_phase=${name}"
  "$@"
}

run_in_dir() {
  local dir="$1"
  shift
  (
    cd "${dir}"
    "$@"
  )
}

run_step axnoded_architecture make -C "${AXNODED_DIR}" check-architecture

run_step axnoded_sandboxd_tests \
  run_in_dir "${AXNODED_DIR}" go test ./internal/api ./internal/service ./internal/runtime/... ./internal/sandboxd/... ./cmd/axern-sandboxd/... ./cmd/verify-sandboxd-provider

run_step sandboxd_provider_smoke make -C "${AXNODED_DIR}" verify-sandboxd-provider-smoke
run_step sandboxd_packaging make -C "${AXNODED_DIR}" verify-sandboxd-packaging
run_step proto_generated_check make -C "${ROOT_DIR}" proto-generated-check

run_step go_sdk_tests run_in_dir "${ROOT_DIR}/sdk/go" go test ./...
run_step python_sdk_pyright run_in_dir "${ROOT_DIR}/sdk/python" uv run --package axern-sdk pyright
run_step python_sdk_capability_tests \
  run_in_dir "${ROOT_DIR}" uv run --with pytest --package axern-sdk python -m pytest sdk/python/tests/test_node_client.py sdk/python/tests/test_sandbox_lifecycle.py -q

echo "sandboxd_release_readiness_ok=true"
