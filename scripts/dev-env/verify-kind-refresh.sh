#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

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
    bash "${ROOT_DIR}/scripts/dev-env/kind-status.sh" || true
    attempt=$((attempt + 1))
    sleep 2
  done
  return "${exit_code}"
}

on_error() {
  local exit_code="$1"
  log "kind refresh verification failed"
  bash "${ROOT_DIR}/scripts/dev-env/kind-status.sh" || true
  exit "${exit_code}"
}

trap 'on_error $?' ERR

cd "${ROOT_DIR}"

log "refreshing repo-managed kind environment"
run_with_retry 2 make kind-refresh

log "running kind smoke suite"
run_with_retry 2 make kind-smoke
run_with_retry 2 make kind-gateway-smoke
run_with_retry 2 make kind-service-volume-smoke
run_with_retry 2 make kind-run-smoke
run_with_retry 2 make kind-server-base-smoke
run_with_retry 2 make kind-quota-smoke

log "checking kind admin reliability"
run_cmd bash -lc 'export K8S_ENV_NAME=kind; source scripts/dev-env/lib.sh; local_smoke_report_reliability "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"'

if [ "${KIND_REFRESH_TUNNEL_E2E:-0}" = "1" ] || [ "${KIND_REFRESH_TUNNEL_E2E:-0}" = "true" ]; then
  run_with_retry 2 make kind-tunnel-e2e
fi
if [ "${KIND_REFRESH_TUNNEL_RELAY_E2E:-0}" = "1" ] || [ "${KIND_REFRESH_TUNNEL_RELAY_E2E:-0}" = "true" ]; then
  run_with_retry 2 make kind-tunnel-relay-e2e
fi
if [ "${KIND_REFRESH_TUNNEL_MULTIRELAY_E2E:-0}" = "1" ] || [ "${KIND_REFRESH_TUNNEL_MULTIRELAY_E2E:-0}" = "true" ]; then
  run_with_retry 2 make kind-tunnel-multirelay-e2e
fi
if [ "${KIND_REFRESH_IMAGE_SERVICE_SMOKE:-0}" = "1" ] || [ "${KIND_REFRESH_IMAGE_SERVICE_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make kind-image-service-smoke
fi
if [ "${KIND_REFRESH_REGISTRY_IMAGE_SMOKE:-0}" = "1" ] || [ "${KIND_REFRESH_REGISTRY_IMAGE_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make kind-axern-registry-image-smoke
fi
if [ "${KIND_REFRESH_AXERN_NYDUS_SMOKE:-0}" = "1" ] || [ "${KIND_REFRESH_AXERN_NYDUS_SMOKE:-0}" = "true" ]; then
  run_with_retry 2 make kind-axern-nydus-smoke
fi

log "kind refresh verification completed"
bash "${ROOT_DIR}/scripts/dev-env/kind-status.sh" || true
echo "kind_refresh_verify_ok=true"
