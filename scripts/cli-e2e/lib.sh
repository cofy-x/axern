#!/usr/bin/env bash

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/runtime/axnoded/scripts/lib/verify-docker-common.sh"

AXERN_BIN="${AXERN_BIN:-${AXERN_ROOT}/bin/axern}"
CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_ADDRESS:-127.0.0.1:24100}"
CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_ADDRESS:-127.0.0.1:24101}"
NODE_GRPC_ADDRESS="${NODE_GRPC_ADDRESS:-127.0.0.1:24010}"
NODE_HTTP_ADDRESS="${NODE_HTTP_ADDRESS:-0.0.0.0:23001}"
GATEWAY_HTTP_ADDRESS="${GATEWAY_HTTP_ADDRESS:-127.0.0.1:25080}"
GATEWAY_CONTROL_ADDRESS="${GATEWAY_CONTROL_ADDRESS:-127.0.0.1:25000}"
GATEWAY_SSH_ADDRESS="${GATEWAY_SSH_ADDRESS:-127.0.0.1:25022}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/shared/run/axnoded.sock}"
# The privileged node creates sandbox0 from this range. Keep it distinct from
# Docker's default 172.17.0.0/16 bridge so host-gateway traffic stays on eth0.
AXNODED_NETWORK_IP_RANGE="${AXNODED_NETWORK_IP_RANGE:-172.31.0.1/16}"
CONTROL_PLANE_NODE_ID="${CONTROL_PLANE_NODE_ID:-node-axern-cli-e2e}"
CONTROL_PLANE_ENROLLMENT_TOKEN="${CONTROL_PLANE_ENROLLMENT_TOKEN:-node-axern-cli-e2e-credential-00000000}"
PYTHON_RUNTIME_IMAGE_REF="${PYTHON_RUNTIME_IMAGE_REF:-python:3.12-slim}"
POSTGRES_CONTAINER_NAME="${POSTGRES_CONTAINER_NAME:-axern-cli-e2e-postgres}"
POSTGRES_NETWORK_NAME="${POSTGRES_NETWORK_NAME:-axern-cli-e2e-net}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axern-cli-e2e-node}"
POSTGRES_DB="${POSTGRES_DB:-axern}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_HOST_PORT="${POSTGRES_HOST_PORT:-25432}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

shared_run_dir="$(mktemp -d)"
cert_dir="$(mktemp -d)"
controld_log="$(mktemp)"
gatewayd_log="$(mktemp)"
cli_config_dir="$(mktemp -d)"
cli_config_file="${cli_config_dir}/config.json"
cli_object_output="$(mktemp)"
cli_wait_output="$(mktemp)"
cli_error_output="$(mktemp)"
docker_secret_file="$(mktemp)"
ssh_dir="$(mktemp -d)"
CONTROLD_PID=""
GATEWAYD_PID=""
AXERN_CLI_E2E_KEEP_ON_FAILURE="${AXERN_CLI_E2E_KEEP_ON_FAILURE:-0}"
AXERN_CLI_E2E_REBUILD_IMAGES="${AXERN_CLI_E2E_REBUILD_IMAGES:-0}"
AXERN_CLI_E2E_REBUILD_VERIFY_IMAGE="${AXERN_CLI_E2E_REBUILD_VERIFY_IMAGE:-1}"
AXERN_CLI_E2E_REBUILD_RUNTIME_IMAGE="${AXERN_CLI_E2E_REBUILD_RUNTIME_IMAGE:-0}"
AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE="${AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE:-1}"
axern_cli_e2e_failed=0
current_e2e_step=""
failed_e2e_step=""

# Keep this e2e hermetic even when the invoking shell has a real Axern
# context selected. The test writes and uses its own temporary config below.
export AXERN_CONFIG="${cli_config_file}"
unset AXERN_CONTEXT
unset AXERN_ENDPOINT
unset AXERN_SSH_ENDPOINT
unset AXERN_SSH_IDENTITY_FILE

split_into_vars() {
  local address="$1"
  local __host_var="$2"
  local __port_var="$3"
  local host port
  host="${address%:*}"
  port="${address##*:}"
  printf -v "${__host_var}" '%s' "${host}"
  printf -v "${__port_var}" '%s' "${port}"
}

tcp_ready() {
  local address="$1"
  local host="${address%:*}"
  local port="${address##*:}"
  bash -c "</dev/tcp/${host}/${port}" >/dev/null 2>&1
}

reserve_e2e_ports() {
  split_into_vars "${CONTROLD_GRPC_ADDRESS}" CONTROLD_GRPC_HOST CONTROLD_GRPC_PORT
  CONTROLD_GRPC_PORT="$(reserve_host_port "${CONTROLD_GRPC_HOST}" "${CONTROLD_GRPC_PORT}")"
  CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_HOST}:${CONTROLD_GRPC_PORT}"

  split_into_vars "${CONTROLD_HTTP_ADDRESS}" CONTROLD_HTTP_HOST CONTROLD_HTTP_PORT
  CONTROLD_HTTP_PORT="$(reserve_host_port "${CONTROLD_HTTP_HOST}" "${CONTROLD_HTTP_PORT}")"
  CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_HOST}:${CONTROLD_HTTP_PORT}"



  split_into_vars "${NODE_GRPC_ADDRESS}" NODE_GRPC_HOST NODE_GRPC_PORT
  NODE_GRPC_PORT="$(reserve_host_port "${NODE_GRPC_HOST}" "${NODE_GRPC_PORT}")"
  NODE_GRPC_ADDRESS="${NODE_GRPC_HOST}:${NODE_GRPC_PORT}"

  split_into_vars "${GATEWAY_HTTP_ADDRESS}" GATEWAY_HTTP_HOST GATEWAY_HTTP_PORT
  GATEWAY_HTTP_PORT="$(reserve_host_port "${GATEWAY_HTTP_HOST}" "${GATEWAY_HTTP_PORT}")"
  GATEWAY_HTTP_ADDRESS="${GATEWAY_HTTP_HOST}:${GATEWAY_HTTP_PORT}"

  split_into_vars "${GATEWAY_CONTROL_ADDRESS}" GATEWAY_CONTROL_HOST GATEWAY_CONTROL_PORT
  GATEWAY_CONTROL_PORT="$(reserve_host_port "${GATEWAY_CONTROL_HOST}" "${GATEWAY_CONTROL_PORT}")"
  GATEWAY_CONTROL_ADDRESS="${GATEWAY_CONTROL_HOST}:${GATEWAY_CONTROL_PORT}"

  split_into_vars "${GATEWAY_SSH_ADDRESS}" GATEWAY_SSH_HOST GATEWAY_SSH_PORT
  GATEWAY_SSH_PORT="$(reserve_host_port "${GATEWAY_SSH_HOST}" "${GATEWAY_SSH_PORT}")"
  GATEWAY_SSH_ADDRESS="${GATEWAY_SSH_HOST}:${GATEWAY_SSH_PORT}"

  POSTGRES_HOST_PORT="$(reserve_host_port "127.0.0.1" "${POSTGRES_HOST_PORT}")"
  CONTROLD_ENROLLMENT_PORT="$(reserve_unique_host_port "${CONTROLD_GRPC_HOST}" 0 \
    "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${NODE_GRPC_PORT}" \
    "${GATEWAY_HTTP_PORT}" "${GATEWAY_CONTROL_PORT}" "${GATEWAY_SSH_PORT}" "${POSTGRES_HOST_PORT}")"
  CONTROLD_POSTGRES_DSN="${CONTROLD_POSTGRES_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_HOST_PORT}/${POSTGRES_DB}?sslmode=disable}"
}

cleanup() {
  if [ "${axern_cli_e2e_failed}" = "1" ]; then
    echo "axern_cli_e2e_step_failed=${failed_e2e_step:-${current_e2e_step:-unknown}}" >&2
  fi
  if [ "${axern_cli_e2e_failed}" = "1" ] && [ "${AXERN_CLI_E2E_KEEP_ON_FAILURE}" = "1" ]; then
    echo "axern-cli-e2e preserving failure artifacts:" >&2
    echo "  controld log: ${controld_log}" >&2
    echo "  cert dir: ${cert_dir}" >&2
    echo "  shared run dir: ${shared_run_dir}" >&2
    return 0
  fi
  if [ -n "${CONTROLD_PID}" ]; then
    kill "${CONTROLD_PID}" >/dev/null 2>&1 || true
    wait "${CONTROLD_PID}" >/dev/null 2>&1 || true
  fi
  if [ -n "${GATEWAYD_PID}" ]; then
    kill "${GATEWAYD_PID}" >/dev/null 2>&1 || true
    wait "${GATEWAYD_PID}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true
  rm -rf "${shared_run_dir}" "${cert_dir}" "${controld_log}" "${gatewayd_log}" "${cli_config_dir}" "${cli_object_output}" "${cli_wait_output}" "${cli_error_output}" "${docker_secret_file}" "${ssh_dir}"
}

dump_logs() {
  axern_cli_e2e_failed=1
  failed_e2e_step="${current_e2e_step:-unknown}"
  if [ -n "${current_e2e_step:-}" ]; then
    echo "--- current e2e step: ${current_e2e_step} ---" >&2
  fi
  echo "--- last cli stderr ---" >&2
  cat "${cli_error_output}" >&2 || true
  echo "--- last cli stdout/object ---" >&2
  cat "${cli_object_output}" >&2 || true
  echo "--- controld log ---" >&2
  cat "${controld_log}" >&2 || true
  dump_controld_endpoint "nodesz"
  dump_controld_endpoint "resourcez"
  dump_controld_endpoint "reconcilez"
  dump_controld_endpoint "allocation-reconcilez"
  dump_controld_endpoint "consistencyz"
  echo "--- gatewayd log ---" >&2
  cat "${gatewayd_log}" >&2 || true
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  dump_node_control_plane_route
  dump_node_log_tail "axnoded" "/var/log/axnoded/axnoded.log" 160
  dump_node_log_tail "imagefsd" "/var/lib/imagemgr/logs/imagefsd.log" 80
  dump_node_log_tail "imagemgr" "/var/lib/imagemgr/logs/imagemgr.log" 120
  dump_node_log_tail "node-tunneld" "/var/log/axnoded/node-tunneld.log" 120
}

dump_controld_endpoint() {
  local path="$1"
  echo "--- controld /${path} ---" >&2
  curl --connect-timeout 2 --max-time 5 -fsS "http://${CONTROLD_HTTP_ADDRESS}/${path}" >&2 || true
  echo >&2
}

dump_node_log_tail() {
  local label="$1"
  local path="$2"
  local lines="$3"
  echo "--- ${label} log tail ---" >&2
  docker cp "${NODE_CONTAINER_NAME}:${path}" - | tar -xOf - | tail -n "${lines}" >&2 || true
}

node_control_plane_tcp_ready() {
  docker exec \
    -e "AXERN_E2E_CONTROL_PLANE_PORT=${CONTROLD_GRPC_ADDRESS##*:}" \
    "${NODE_CONTAINER_NAME}" \
    bash -lc 'timeout 5 bash -lc "exec 3<>/dev/tcp/host.docker.internal/${AXERN_E2E_CONTROL_PLANE_PORT}"' \
    >/dev/null 2>&1
}

dump_node_control_plane_route() {
  echo "--- node to control-plane route ---" >&2
  docker exec \
    -e "AXERN_E2E_CONTROL_PLANE_PORT=${CONTROLD_GRPC_ADDRESS##*:}" \
    "${NODE_CONTAINER_NAME}" \
    bash -lc '
      set +e
      printf "%s\n" "hosts:"
      getent ahostsv4 host.docker.internal
      printf "%s\n" "routes:"
      ip -4 route show
      if timeout 5 bash -lc "exec 3<>/dev/tcp/host.docker.internal/${AXERN_E2E_CONTROL_PLANE_PORT}"; then
        printf "%s\n" "control_plane_tcp_ready=true"
      else
        printf "control_plane_tcp_ready=false rc=%s\n" "$?"
      fi
      if command -v openssl >/dev/null 2>&1; then
        timeout 10 openssl s_client \
          -connect "host.docker.internal:${AXERN_E2E_CONTROL_PLANE_PORT}" \
          -servername host.docker.internal \
          -CAfile /shared/certs/ca.crt \
          -cert /var/lib/axnoded/root/identity/node.pem \
          -key /var/lib/axnoded/root/identity/node.pem \
          -verify_return_error \
          -verify_hostname host.docker.internal \
          -brief </dev/null
        printf "control_plane_tls_probe_rc=%s\n" "$?"
      else
        printf "%s\n" "control_plane_tls_probe=openssl-unavailable"
      fi
    ' >&2 || true
}

run_step() {
  local step="$1"
  shift

  current_e2e_step="${step}"
  echo "axern_cli_e2e_step_start=${step}" >&2
  "$@"
  if [ "${failed_e2e_step}" = "${step}" ]; then
    axern_cli_e2e_failed=0
    failed_e2e_step=""
  fi
  echo "axern_cli_e2e_step_ok=${step}" >&2
  current_e2e_step=""
}

json_query() {
  local label="$1"
  local query="$2"
  local input="$3"
  local result

  if [ -z "${input}" ]; then
    echo "${label} returned empty output" >&2
    dump_logs
    exit 1
  fi

  if ! result="$(python3 -c "import json,sys; print(${query})" <<<"${input}" 2>"${cli_error_output}")"; then
    echo "${label} did not return valid JSON" >&2
    cat "${cli_error_output}" >&2 || true
    echo "--- raw ${label} output ---" >&2
    printf '%s\n' "${input}" >&2
    dump_logs
    exit 1
  fi

  printf '%s\n' "${result}"
}

wait_for_running_run_allocation() {
  local run_id="$1"
  local label="$2"
  local timeout_seconds="${3:-120}"
  local deadline run_get_json allocation_id
  if ! [[ "${timeout_seconds}" =~ ^[0-9]+$ ]]; then
    echo "${label} wait timeout must be numeric seconds, got ${timeout_seconds}" >&2
    dump_logs
    return 1
  fi
  deadline=$((SECONDS + timeout_seconds))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    run_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run get "${run_id}" -o json 2>/dev/null || true)"
    if [ -n "${run_get_json}" ] && python3 -c 'import json,sys; run=json.load(sys.stdin)["run"]; sys.exit(0 if run.get("status") == "running" and run.get("allocation_id") else 1)' <<<"${run_get_json}" >/dev/null 2>&1; then
      allocation_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["allocation_id"])' <<<"${run_get_json}")"
      if [ -n "${allocation_id}" ]; then
        printf '%s\n' "${allocation_id}"
        return 0
      fi
    fi
    if [ -n "${run_get_json}" ] && python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["run"].get("status") in ("failed", "succeeded", "cancelled") else 1)' <<<"${run_get_json}" >/dev/null 2>&1; then
      echo "${label} reached a terminal state before running: ${run_get_json}" >&2
      dump_logs
      return 1
    fi
    sleep 2
  done
  echo "${label} did not surface a running allocation in time" >&2
  dump_logs
  return 1
}

wait_for_postgres() {
  local deadline
  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if docker exec "${POSTGRES_CONTAINER_NAME}" pg_isready -U "${POSTGRES_USER}" >/dev/null 2>&1 &&
      docker exec "${POSTGRES_CONTAINER_NAME}" psql -U "${POSTGRES_USER}" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" 2>/dev/null | grep -qx '1'; then
      return 0
    fi
    sleep 1
  done
  return 1
}

ensure_verify_image_once() {
  if [ "${AXERN_CLI_E2E_REBUILD_IMAGES}" != "1" ] && [ "${AXERN_CLI_E2E_REBUILD_VERIFY_IMAGE}" != "1" ] && docker image inspect "${IMAGE_TAG}" >/dev/null 2>&1; then
    echo "reusing verify image ${IMAGE_TAG}" >&2
    return 0
  fi
  echo "building verify image ${IMAGE_TAG}" >&2
  ensure_verify_image
}

ensure_python_runtime_image_once() {
  if [ "${AXERN_CLI_E2E_REBUILD_IMAGES}" != "1" ] && [ "${AXERN_CLI_E2E_REBUILD_RUNTIME_IMAGE}" != "1" ] && docker image inspect "${PYTHON_RUNTIME_IMAGE_REF}" >/dev/null 2>&1; then
    echo "reusing runtime image ${PYTHON_RUNTIME_IMAGE_REF}" >&2
    return 0
  fi
  echo "pulling test image ${PYTHON_RUNTIME_IMAGE_REF}" >&2
  docker pull --platform "${VERIFY_DOCKER_PLATFORM}" "${PYTHON_RUNTIME_IMAGE_REF}" >/dev/null
}

import_python_runtime_image_once() {
  import_oci_image_to_node "${PYTHON_RUNTIME_IMAGE_REF}" "${NODE_CONTAINER_NAME}"
}
