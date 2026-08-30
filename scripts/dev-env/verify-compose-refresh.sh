#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=./lib.sh
source "${ROOT_DIR}/scripts/dev-env/lib.sh"

timestamp() {
  date '+%Y-%m-%d %H:%M:%S'
}

log() {
  printf '[%s] %s\n' "$(timestamp)" "$*"
}

run_cmd() {
  printf '+'
  for arg in "$@"; do
    printf ' %q' "${arg}"
  done
  printf '\n'
  "$@"
}

run_with_retry() {
  local attempts="$1"
  shift
  local attempt=1
  local exit_code=0
  while [ "${attempt}" -le "${attempts}" ]; do
    if run_cmd "$@"; then
      return 0
    else
      exit_code=$?
    fi
    if [ "${attempt}" -ge "${attempts}" ]; then
      return "${exit_code}"
    fi
    log "command failed on attempt ${attempt}/${attempts}; retrying"
    bash "${ROOT_DIR}/scripts/dev-env/compose-status.sh" || true
    attempt=$((attempt + 1))
    sleep 2
  done
  return "${exit_code}"
}

on_error() {
  local exit_code="$1"
  log "compose refresh verification failed"
  bash "${ROOT_DIR}/scripts/dev-env/compose-status.sh" || true
  exit "${exit_code}"
}

trap 'on_error $?' ERR

cd "${ROOT_DIR}"

log "refreshing local compose environment"
run_with_retry 2 make local-compose-refresh

log "running compose smoke suite"
run_with_retry 2 make local-compose-smoke
run_with_retry 2 make local-compose-doctor-smoke
run_with_retry 2 make local-compose-dns-doctor-smoke
run_with_retry 2 make local-compose-gateway-smoke
run_with_retry 2 make local-compose-service-volume-smoke
run_with_retry 2 make local-compose-run-smoke
run_with_retry 2 make local-compose-server-base-smoke
run_with_retry 2 make local-compose-quota-smoke
run_with_retry 2 make local-compose-python-sdk-e2e
run_with_retry 2 make local-compose-go-sdk-e2e

log "checking compose admin repair actions"
run_cmd bash -lc 'source scripts/dev-env/lib.sh; local_smoke_assert_compose_admin_repair_actions "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"'

log "checking compose admin reliability"
run_cmd bash -lc 'source scripts/dev-env/lib.sh; local_smoke_report_reliability compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"'

if [ "${COMPOSE_REFRESH_TUNNEL_E2E:-0}" = "1" ] || [ "${COMPOSE_REFRESH_TUNNEL_E2E:-0}" = "true" ]; then
  run_with_retry 2 make local-compose-tunnel-e2e
fi
if [ "${COMPOSE_REFRESH_TUNNEL_BENCHMARK:-0}" = "1" ] || [ "${COMPOSE_REFRESH_TUNNEL_BENCHMARK:-0}" = "true" ]; then
  run_with_retry 2 make tunnel-benchmark-compose
fi
if [ "${COMPOSE_REFRESH_IMAGE_SERVICE_SMOKE:-0}" = "1" ] || [ "${COMPOSE_REFRESH_IMAGE_SERVICE_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make local-compose-image-service-smoke
fi
if [ "${LOCAL_COMPOSE_REGISTRY_IMAGE_SMOKE:-0}" = "1" ] || [ "${LOCAL_COMPOSE_REGISTRY_IMAGE_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make local-compose-registry-image-smoke
fi
if [ "${LOCAL_COMPOSE_AXERN_NYDUS_SMOKE:-0}" = "1" ] || [ "${LOCAL_COMPOSE_AXERN_NYDUS_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make local-compose-nydus-smoke
fi

log "waiting for compose allocation cleanup convergence"
run_cmd bash -lc 'source scripts/dev-env/lib.sh; local_smoke_wait_for_compose_allocation_cleanup 120'

log "compose refresh verification completed"
bash "${ROOT_DIR}/scripts/dev-env/compose-status.sh" || true
echo "compose_refresh_verify_ok=true"
