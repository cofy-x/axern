#!/usr/bin/env bash
set -euo pipefail

ROOTDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOTDIR}/scripts/dev-env/lib.sh"

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
  bash "${ROOTDIR}/scripts/dev-env/compose-status.sh" || true
  log "kind status snapshot"
  bash "${ROOTDIR}/scripts/dev-env/kind-status.sh" || true
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
  log "local storage verification failed"
  print_status
  exit "${exit_code}"
}

trap 'on_error $?' ERR

cd "${ROOTDIR}"

log "starting local storage verification"
run_with_retry 2 make local-compose-service-volume-smoke
run_with_retry 2 make kind-service-volume-smoke
log "local storage verification completed"
echo "local_storage_verify_ok=true"
