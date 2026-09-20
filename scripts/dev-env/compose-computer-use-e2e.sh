#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd uv

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

computer_use_image="${AXERN_TEST_COMPUTER_USE_IMAGE:-axern/test-computer-use:dev}"
headless_image="${AXERN_TEST_HEADLESS_IMAGE:-python:3.12-slim}"
fixture_dir="${AXERN_ROOT}/sdk/python/tests/e2e/fixtures/computer-use"

# Computer Use is a generic Allocation capability. Its only curated userspace
# image is deliberately test-owned and built by this explicit E2E; Axern does
# not publish or preload workload runtime images.
docker build --tag "${computer_use_image}" "${fixture_dir}"
docker image inspect "${headless_image}" >/dev/null 2>&1 || docker pull "${headless_image}"
computer_use_image="$(local_smoke_prepare_compose_image "${computer_use_image}")"
headless_image="$(local_smoke_prepare_compose_image "${headless_image}")"

uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/computer_use_e2e.py" \
  --endpoint "${AXERN_ENDPOINT}" \
  --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
  --tls-cert "${AXERN_TLS_CERT}" \
  --tls-key "${AXERN_TLS_KEY}" \
  --node-container "${node_container}" \
  --computer-use-image "${computer_use_image}" \
  --headless-image "${headless_image}"

echo "compose_computer_use_e2e_ok=true"
