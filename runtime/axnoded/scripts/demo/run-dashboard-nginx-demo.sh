#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

DASHBOARD_HOST_PORT="${DASHBOARD_HOST_PORT:-23001}"
CONTAINER_HTTP_PORT="${CONTAINER_HTTP_PORT:-23001}"
DEMO_CONTAINER_NAME="${DEMO_CONTAINER_NAME:-axnoded-dashboard-nginx-demo}"
READY_TIMEOUT="${READY_TIMEOUT:-180}"
KEEP_RUNNING="${KEEP_RUNNING:-true}"
PRESERVE_ON_FAILURE="${PRESERVE_ON_FAILURE:-true}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
RUNSC_HOST_PORT="${RUNSC_HOST_PORT:-18080}"
RUNC_HOST_PORT="${RUNC_HOST_PORT:-18081}"
cleanup_on_exit=1

cleanup_demo_container() {
  docker rm -f "${DEMO_CONTAINER_NAME}" >/dev/null 2>&1 || true
}

cleanup_demo() {
  if [ "${cleanup_on_exit}" = "1" ]; then
    cleanup_demo_container
  fi
}

port_is_available() {
  local port="$1"
  python3 - "${port}" <<'PY'
import socket
import sys

port = int(sys.argv[1])
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    try:
        sock.bind(("0.0.0.0", port))
    except OSError:
        raise SystemExit(1)
PY
}

require_distinct_host_ports() {
  if [ "${DASHBOARD_HOST_PORT}" = "${RUNSC_HOST_PORT}" ] ||
    [ "${DASHBOARD_HOST_PORT}" = "${RUNC_HOST_PORT}" ] ||
    [ "${RUNSC_HOST_PORT}" = "${RUNC_HOST_PORT}" ]; then
    echo "dashboard demo host ports must be distinct:" >&2
    echo "  DASHBOARD_HOST_PORT=${DASHBOARD_HOST_PORT}" >&2
    echo "  RUNSC_HOST_PORT=${RUNSC_HOST_PORT}" >&2
    echo "  RUNC_HOST_PORT=${RUNC_HOST_PORT}" >&2
    exit 1
  fi
}

require_host_port_available() {
  local name="$1"
  local port="$2"
  if port_is_available "${port}"; then
    return 0
  fi

  echo "dashboard demo cannot bind ${name} host port ${port}; choose a free port and retry." >&2
  echo "From runtime/axnoded:" >&2
  echo "  ${name}=<free-port> make run-dashboard-nginx-demo" >&2
  echo "From the repository root:" >&2
  echo "  ${name}=<free-port> make axnoded-run-dashboard-nginx-demo" >&2
  exit 1
}

require_demo_host_ports_available() {
  require_distinct_host_ports
  require_host_port_available DASHBOARD_HOST_PORT "${DASHBOARD_HOST_PORT}"
  require_host_port_available RUNSC_HOST_PORT "${RUNSC_HOST_PORT}"
  require_host_port_available RUNC_HOST_PORT "${RUNC_HOST_PORT}"
}

trap cleanup_demo EXIT

cleanup_demo_container
require_demo_host_ports_available
ensure_verify_image

docker run -d \
  --name "${DEMO_CONTAINER_NAME}" \
  --privileged \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -e "NAT_BACKEND=${NAT_BACKEND}" \
  -p "${DASHBOARD_HOST_PORT}:${CONTAINER_HTTP_PORT}" \
  -p "${RUNSC_HOST_PORT}:${RUNSC_HOST_PORT}" \
  -p "${RUNC_HOST_PORT}:${RUNC_HOST_PORT}" \
  "${IMAGE_TAG}" \
  bash /workspace/scripts/demo/run-dashboard-nginx-demo-in-container.sh >/dev/null

started_at=${SECONDS}
deadline=$((started_at + READY_TIMEOUT))
next_progress=$((SECONDS + 15))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  code="$(curl -s --connect-timeout 1 --max-time 2 \
    -o /tmp/axnoded-dashboard-nginx-demo.out -w '%{http_code}' \
    "http://127.0.0.1:${DASHBOARD_HOST_PORT}/demo/nginx" 2>/dev/null || true)"
  if [ "${code}" = "200" ]; then
    echo "dashboard_demo_ok=true"
    echo "demo_container=${DEMO_CONTAINER_NAME}"
    echo "demo_backend=${NAT_BACKEND}"
    echo "dashboard_url=http://127.0.0.1:${DASHBOARD_HOST_PORT}/"
    echo "dashboard_nginx_url=http://127.0.0.1:${DASHBOARD_HOST_PORT}/demo/nginx"
    echo "managed_instances=page_controls"
    if [ "${NAT_BACKEND}" = "iptables" ]; then
      echo "runsc_demo_url=http://127.0.0.1:${RUNSC_HOST_PORT}/"
      echo "runc_demo_url=http://127.0.0.1:${RUNC_HOST_PORT}/"
    else
      echo "runsc_demo_url_best_effort=http://127.0.0.1:${RUNSC_HOST_PORT}/"
      echo "runc_demo_url_best_effort=http://127.0.0.1:${RUNC_HOST_PORT}/"
      echo "demo_host_port_note=best_effort_only_for_non_iptables_backend"
    fi
    if [ "${KEEP_RUNNING}" = "true" ]; then
      cleanup_on_exit=0
      echo "demo_running=true"
    fi
    exit 0
  fi
  if ! docker ps --filter "name=${DEMO_CONTAINER_NAME}" --filter "status=running" --format '{{.Names}}' | grep -qx "${DEMO_CONTAINER_NAME}"; then
    echo "dashboard demo container exited before becoming ready" >&2
    break
  fi
  if [ "${SECONDS}" -ge "${next_progress}" ]; then
    echo "waiting_for_dashboard=true host_port=${DASHBOARD_HOST_PORT} elapsed_seconds=$((SECONDS - started_at))" >&2
    next_progress=$((SECONDS + 15))
  fi
  sleep 1
done

echo "dashboard demo failed on host port ${DASHBOARD_HOST_PORT}" >&2
echo "hint: first startup may spend time compiling axnoded inside the demo container; increase READY_TIMEOUT if needed" >&2
if [ "${PRESERVE_ON_FAILURE}" = "true" ]; then
  cleanup_on_exit=0
  echo "demo_preserved=true" >&2
fi
echo "--- docker logs ---" >&2
docker logs "${DEMO_CONTAINER_NAME}" >&2 || true
echo "--- volumed log tail ---" >&2
docker exec "${DEMO_CONTAINER_NAME}" tail -n 120 /tmp/volumed-dashboard.log >&2 || true
echo "--- axnoded log tail ---" >&2
docker exec "${DEMO_CONTAINER_NAME}" tail -n 120 /tmp/axnoded-dashboard.log >&2 || true
exit 1
