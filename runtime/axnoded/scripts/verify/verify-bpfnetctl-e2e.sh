#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

DEMO_CONTAINER_NAME="${DEMO_CONTAINER_NAME:-axnoded-bpfnetctl-e2e}"
DASHBOARD_HOST="${DASHBOARD_HOST:-127.0.0.1}"
DASHBOARD_HOST_PORT="${DASHBOARD_HOST_PORT:-$(reserve_host_port 0.0.0.0 23011)}"
CONTAINER_HTTP_PORT="${CONTAINER_HTTP_PORT:-23001}"
RUNSC_HOST_PORT="${RUNSC_HOST_PORT:-$(reserve_host_port 0.0.0.0 18080)}"
RUNC_HOST_PORT="${RUNC_HOST_PORT:-$(reserve_host_port 0.0.0.0 18081)}"
READY_TIMEOUT="${READY_TIMEOUT:-180}"
PRESERVE_ON_FAILURE="${PRESERVE_ON_FAILURE:-false}"
NAT_BACKEND="ebpf"
cleanup_on_exit=1

cleanup_demo_container() {
  docker rm -f "${DEMO_CONTAINER_NAME}" >/dev/null 2>&1 || true
}

cleanup_demo() {
  if [ "${cleanup_on_exit}" = "1" ]; then
    cleanup_demo_container
  fi
}

preserve_on_failure() {
  if [ "${PRESERVE_ON_FAILURE}" = "true" ]; then
    cleanup_on_exit=0
    echo "demo_preserved=true" >&2
    echo "demo_container=${DEMO_CONTAINER_NAME}" >&2
  fi
}

fail_with_logs() {
  echo "$*" >&2
  preserve_on_failure
  echo "--- docker logs ---" >&2
  docker logs "${DEMO_CONTAINER_NAME}" >&2 || true
  echo "--- volumed log tail ---" >&2
  docker exec "${DEMO_CONTAINER_NAME}" tail -n 160 /tmp/volumed-dashboard.log >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${DEMO_CONTAINER_NAME}" tail -n 160 /tmp/axnoded-dashboard.log >&2 || true
  exit 1
}

wait_for_dashboard() {
  local started_at deadline next_progress code
  started_at=${SECONDS}
  deadline=$((started_at + READY_TIMEOUT))
  next_progress=$((SECONDS + 15))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    code="$(curl -s --connect-timeout 1 --max-time 2 \
      -o /tmp/axnoded-bpfnetctl-e2e.out -w '%{http_code}' \
      "http://${DASHBOARD_HOST}:${DASHBOARD_HOST_PORT}/demo/nginx" 2>/dev/null || true)"
    if [ "${code}" = "200" ]; then
      echo "dashboard_demo_ok=true"
      return 0
    fi
    if ! docker ps --filter "name=${DEMO_CONTAINER_NAME}" --filter "status=running" --format '{{.Names}}' | grep -qx "${DEMO_CONTAINER_NAME}"; then
      return 1
    fi
    if [ "${SECONDS}" -ge "${next_progress}" ]; then
      echo "waiting_for_dashboard=true host_port=${DASHBOARD_HOST_PORT} elapsed_seconds=$((SECONDS - started_at))" >&2
      next_progress=$((SECONDS + 15))
    fi
    sleep 1
  done
  return 1
}

assert_check_json() {
  local label="$1"
  local json_file stderr_file
  json_file="$(mktemp)"
  stderr_file="$(mktemp)"

  if ! docker exec "${DEMO_CONTAINER_NAME}" bpfnetctl check --json >"${json_file}" 2>"${stderr_file}"; then
    echo "--- bpfnetctl check --json (${label}) stderr ---" >&2
    cat "${stderr_file}" >&2
    echo "--- bpfnetctl check --json (${label}) stdout ---" >&2
    cat "${json_file}" >&2
    rm -f "${json_file}" "${stderr_file}"
    fail_with_logs "bpfnetctl check --json failed during ${label}"
  fi

  if ! jq -e '.ok == true' "${json_file}" >/dev/null; then
    echo "--- bpfnetctl check --json (${label}) ---" >&2
    cat "${json_file}" >&2
    rm -f "${json_file}" "${stderr_file}"
    fail_with_logs "bpfnetctl check reported ok=false during ${label}"
  fi

  if ! jq -e '
    ([.checks[] | select(.name == "pinned_programs" and .ok == true)] | length == 1) and
    ([.checks[] | select(.name | startswith("program:"))] | length > 0) and
    ([.checks[] | select(.name == "pinned_programs" or (.name | startswith("program:"))) | select(.ok != true)] | length == 0)
  ' "${json_file}" >/dev/null; then
    echo "--- bpfnetctl check --json (${label}) ---" >&2
    cat "${json_file}" >&2
    rm -f "${json_file}" "${stderr_file}"
    fail_with_logs "bpfnetctl program pin checks failed during ${label}"
  fi

  rm -f "${json_file}" "${stderr_file}"
  echo "bpfnetctl_check_${label}_ok=true"
}

start_demo_instance() {
  local runtime="$1"
  # The dashboard handler has a 90-second action deadline. Keep the client
  # bounded while allowing a small margin for the HTTP response.
  curl -fsS --connect-timeout 2 --max-time 95 -X POST \
    -d "runtime=${runtime}&action=start" \
    "http://${DASHBOARD_HOST}:${DASHBOARD_HOST_PORT}/demo/nginx" >/dev/null
}

wait_for_instances() {
  local deadline listing
  deadline=$((SECONDS + READY_TIMEOUT))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    listing="$(docker exec "${DEMO_CONTAINER_NAME}" axctl sandbox list 2>/dev/null || true)"
    if printf '%s\n' "${listing}" | grep -q '^dashboard-nginx-runsc[[:space:]].*RUNNING' &&
      printf '%s\n' "${listing}" | grep -q '^dashboard-nginx-runc[[:space:]].*RUNNING'; then
      echo "dashboard_instances_running=true"
      return 0
    fi
    sleep 1
  done
  echo "--- axctl sandbox list ---" >&2
  docker exec "${DEMO_CONTAINER_NAME}" axctl sandbox list >&2 || true
  return 1
}

trap cleanup_demo EXIT

ensure_verify_image
cleanup_demo_container

docker run -d \
  --name "${DEMO_CONTAINER_NAME}" \
  --privileged \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -e "NAT_BACKEND=${NAT_BACKEND}" \
  -p "${DASHBOARD_HOST}:${DASHBOARD_HOST_PORT}:${CONTAINER_HTTP_PORT}" \
  -p "127.0.0.1:${RUNSC_HOST_PORT}:${RUNSC_HOST_PORT}" \
  -p "127.0.0.1:${RUNC_HOST_PORT}:${RUNC_HOST_PORT}" \
  "${IMAGE_TAG}" \
  bash /workspace/scripts/demo/run-dashboard-nginx-demo-in-container.sh >/dev/null

if ! wait_for_dashboard; then
  fail_with_logs "dashboard demo failed on host port ${DASHBOARD_HOST_PORT}"
fi

assert_check_json "before_instances"

if ! start_demo_instance "runsc"; then
  fail_with_logs "failed to start managed runsc nginx instance"
fi
if ! start_demo_instance "runc"; then
  fail_with_logs "failed to start managed runc nginx instance"
fi

if ! wait_for_instances; then
  fail_with_logs "managed runsc/runc nginx instances did not become RUNNING"
fi

assert_check_json "after_instances"

echo "bpfnetctl_e2e_ok=true"
