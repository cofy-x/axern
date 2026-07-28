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

print_status() {
  log "compose status snapshot"
  bash "${ROOT_DIR}/scripts/dev-env/compose-status.sh" || true
  log "kind status snapshot"
  bash "${ROOT_DIR}/scripts/dev-env/kind-status.sh" || true
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

on_error() {
  local exit_code="$1"
  log "local refresh verification failed"
  print_status
  exit "${exit_code}"
}

trap 'on_error $?' ERR

cd "${ROOT_DIR}"

log "running compose refresh verification"
run_with_retry 2 make local-compose-refresh-verify

log "running kind refresh verification"
run_with_retry 2 make kind-refresh-verify

log "local refresh verification completed"
print_status
echo "local_refresh_verify_ok=true"
