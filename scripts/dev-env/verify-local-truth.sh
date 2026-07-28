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
    print_status
    attempt=$((attempt + 1))
    sleep 2
  done
  return "${exit_code}"
}

print_status() {
  log "compose status snapshot"
  bash "${ROOT_DIR}/scripts/dev-env/compose-status.sh" || true
  log "kind status snapshot"
  bash "${ROOT_DIR}/scripts/dev-env/kind-status.sh" || true
}

on_error() {
  local exit_code="$1"
  log "local truth verification failed"
  print_status
  exit "${exit_code}"
}

trap 'on_error $?' ERR

cd "${ROOT_DIR}"

log "building shared local images"
run_with_retry 2 make local-images-build

log "resetting repo-managed kind environment"
run_with_retry 2 env AXERN_SKIP_LOCAL_IMAGES_BUILD=1 make kind-reset

log "resetting local compose environment"
run_with_retry 2 env AXERN_SKIP_LOCAL_IMAGES_BUILD=1 make local-compose-reset

log "running compose smoke suite"
run_with_retry 2 make local-compose-smoke
run_with_retry 2 make local-compose-gateway-smoke
run_with_retry 2 make local-compose-service-volume-smoke
run_with_retry 2 make local-compose-run-smoke
run_with_retry 2 make local-compose-server-base-smoke
run_with_retry 2 make local-compose-quota-smoke
run_with_retry 2 make local-compose-tunnel-e2e
run_with_retry 2 make local-compose-python-sdk-e2e
run_with_retry 2 make tunnel-benchmark-compose
run_with_retry 2 make local-compose-image-service-smoke

log "running kind smoke suite"
run_with_retry 2 make kind-smoke
run_with_retry 2 make kind-gateway-smoke
run_with_retry 2 make kind-service-volume-smoke
run_with_retry 2 make kind-run-smoke
run_with_retry 2 make kind-server-base-smoke
run_with_retry 2 make kind-quota-smoke
run_with_retry 2 make kind-tunnel-e2e
run_with_retry 2 make kind-tunnel-relay-e2e
run_with_retry 2 make kind-tunnel-multirelay-e2e
run_with_retry 2 make kind-image-service-smoke

log "local truth verification completed"
print_status
echo "local_truth_verify_ok=true"
