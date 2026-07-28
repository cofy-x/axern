#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEV_DIR="${ROOT_DIR}/.dev"
STACK_DIR="${DEV_DIR}/stack"
RUN_DIR="${DEV_DIR}/run"
LOG_DIR="${STACK_DIR}/logs"
PID_DIR="${STACK_DIR}/pids"
BIN_DIR="${STACK_DIR}/bin"
GATEWAY_DASHBOARD_VENDOR_DIR="${ROOT_DIR}/gateway/gatewayd/internal/api/http/dashboard/vendor"
POSTGRES_DATA_DIR="${STACK_DIR}/postgres"

POSTGRES_HOST="${POSTGRES_HOST:-127.0.0.1}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_DB="${POSTGRES_DB:-axern}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_DSN="${POSTGRES_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"

AXERN_DEV_CONTROL_PLANE_TARGET="${AXERN_DEV_CONTROL_PLANE_TARGET:-127.0.0.1:24000}"
AXERN_DEV_CONTROL_PLANE_NODE_ID="${AXERN_DEV_CONTROL_PLANE_NODE_ID:-axern-dev-node}"
AXERN_DEV_CONTROL_PLANE_NODE_TARGET="${AXERN_DEV_CONTROL_PLANE_NODE_TARGET:-127.0.0.1:23000}"
AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN="${AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN:-axern-local-node-token}"
AXERN_DEV_TOKEN="${AXERN_DEV_TOKEN:-axern-local-dev}"
AXERN_SECRETS_MASTER_KEY="${AXERN_SECRETS_MASTER_KEY:-local-only-master-key-32-bytes!!}"

usage() {
  cat >&2 <<'EOF'
Usage:
  scripts/devbox/stack.sh up
  scripts/devbox/stack.sh status
  scripts/devbox/stack.sh down
  scripts/devbox/stack.sh postgres-up
  scripts/devbox/stack.sh postgres-down
  scripts/devbox/stack.sh migrate
  scripts/devbox/stack.sh restart SERVICE
  scripts/devbox/stack.sh logs [SERVICE|all]
  scripts/devbox/stack.sh reset

Start Axern's standalone dev stack inside the Linux devbox.

Services:
  postgres, storaged, controld, tunneld, imagefsd, imagemgr, volumed, axnoded, node-tunneld, gatewayd
EOF
}

services() {
  printf '%s\n' postgres storaged controld tunneld imagefsd imagemgr volumed axnoded node-tunneld gatewayd
}

ensure_linux() {
  if [ "$(uname -s)" != "Linux" ]; then
    echo "dev-stack requires a Linux workspace. Run it inside make devbox-up." >&2
    exit 1
  fi
}

ensure_dirs() {
  mkdir -p "${STACK_DIR}" "${RUN_DIR}" "${LOG_DIR}" "${PID_DIR}" "${BIN_DIR}"
}

pid_file() {
  printf '%s/%s.pid' "${PID_DIR}" "$1"
}

is_running() {
  local name="$1"
  local file
  file="$(pid_file "${name}")"
  [ -f "${file}" ] && kill -0 "$(cat "${file}")" >/dev/null 2>&1
}

wait_tcp() {
  local host="$1"
  local port="$2"
  local name="$3"

  for _ in $(seq 1 480); do
    if nc -z "${host}" "${port}" >/dev/null 2>&1; then
      return
    fi
    sleep 0.25
  done

  echo "${name} did not start on ${host}:${port}" >&2
  return 1
}

wait_unix_socket() {
  local socket="$1"
  local name="$2"

  for _ in $(seq 1 480); do
    if [ -S "${socket}" ]; then
      return
    fi
    sleep 0.25
  done

  echo "${name} did not create ${socket}" >&2
  return 1
}

tcp_status() {
  local host="$1"
  local port="$2"
  if nc -z "${host}" "${port}" >/dev/null 2>&1; then
    printf 'tcp:%s=ok' "${port}"
  else
    printf 'tcp:%s=down' "${port}"
  fi
}

unix_socket_status() {
  local socket="$1"
  local state="${2:-running}"
  if [ -S "${socket}" ]; then
    if [ "${state}" = "running" ]; then
      printf 'socket=ok'
    else
      printf 'socket=stale'
    fi
  else
    printf 'socket=down'
  fi
}

service_health() {
  local name="$1"
  local state="${2:-running}"
  case "${name}" in
    postgres)
      tcp_status "${POSTGRES_HOST}" "${POSTGRES_PORT}"
      ;;
    controld)
      printf '%s %s' "$(tcp_status 127.0.0.1 24000)" "$(tcp_status 127.0.0.1 24001)"
      ;;
    storaged)
      printf '%s %s' "$(tcp_status 127.0.0.1 24020)" "$(tcp_status 127.0.0.1 24021)"
      ;;
    tunneld)
      tcp_status 127.0.0.1 24100
      ;;
    imagefsd)
      unix_socket_status "${RUN_DIR}/imagefsd-chunk.sock" "${state}"
      ;;
    imagemgr)
      unix_socket_status "${RUN_DIR}/imagemgr.sock" "${state}"
      ;;
    volumed)
      unix_socket_status "${RUN_DIR}/volumed.sock" "${state}"
      ;;
    axnoded)
      printf '%s %s %s' "$(tcp_status 127.0.0.1 23000)" "$(tcp_status 127.0.0.1 23001)" "$(unix_socket_status "${RUN_DIR}/axnoded.sock" "${state}")"
      ;;
    gatewayd)
      printf '%s %s' "$(tcp_status 127.0.0.1 25000)" "$(tcp_status 127.0.0.1 25080)"
      ;;
    node-tunneld)
      printf 'no-local-listener'
      ;;
  esac
}

start_service() {
  local name="$1"
  local command="$2"
  local file
  local log_file
  file="$(pid_file "${name}")"
  log_file="${LOG_DIR}/${name}.log"

  if is_running "${name}"; then
    echo "${name} already running (pid $(cat "${file}"))"
    return
  fi

  rm -f "${file}"
  setsid bash -lc "${command}" >"${log_file}" 2>&1 &
  echo "$!" > "${file}"
  echo "started ${name} (pid $(cat "${file}"), log ${log_file})"
}

stop_service() {
  local name="$1"
  local file
  file="$(pid_file "${name}")"

  if ! is_running "${name}"; then
    rm -f "${file}"
    echo "${name} is not running"
    return
  fi

  local pid
  pid="$(cat "${file}")"
  kill "-${pid}" >/dev/null 2>&1 || true
  kill "${pid}" >/dev/null 2>&1 || true
  sudo -n kill "-${pid}" >/dev/null 2>&1 || true
  sudo -n kill "${pid}" >/dev/null 2>&1 || true
  for _ in $(seq 1 40); do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      rm -f "${file}"
      echo "stopped ${name}"
      return
    fi
    sleep 0.25
  done
  sudo -n kill -TERM "-${pid}" >/dev/null 2>&1 || true
  sudo -n kill -TERM "${pid}" >/dev/null 2>&1 || true
  rm -f "${file}"
  echo "stopped ${name}"
}

stop_matching_processes() {
  local pattern="$1"
  local pid
  while IFS= read -r pid; do
    if [ -z "${pid}" ] || [ "${pid}" = "$$" ] || [ "${pid}" = "${PPID}" ]; then
      continue
    fi
    kill "${pid}" >/dev/null 2>&1 || true
    sudo -n kill "${pid}" >/dev/null 2>&1 || true
  done < <(pgrep -f "${pattern}" 2>/dev/null || true)
}

normalize_service() {
  case "${1:-}" in
    postgres|storaged|controld|tunneld|imagefsd|imagemgr|axnoded|node-tunneld|gatewayd)
      printf '%s\n' "$1"
      ;;
    "")
      echo "missing service name" >&2
      usage
      exit 2
      ;;
    *)
      echo "unknown service: $1" >&2
      usage
      exit 2
      ;;
  esac
}

postgres_running() {
  [ -d "${POSTGRES_DATA_DIR}" ] && pg_ctl -D "${POSTGRES_DATA_DIR}" status >/dev/null 2>&1
}

start_postgres() {
  export PGPASSWORD="${POSTGRES_PASSWORD}"

  if [ ! -d "${POSTGRES_DATA_DIR}/base" ]; then
    initdb -D "${POSTGRES_DATA_DIR}" --username="${POSTGRES_USER}" --auth=trust --encoding=UTF8 >/dev/null
  fi

  if postgres_running; then
    echo "postgres already running"
  else
    pg_ctl -D "${POSTGRES_DATA_DIR}" \
      -l "${LOG_DIR}/postgres.log" \
      -o "-c listen_addresses=${POSTGRES_HOST} -p ${POSTGRES_PORT} -c unix_socket_directories=${RUN_DIR}" \
      start >/dev/null
    echo "started postgres (data ${POSTGRES_DATA_DIR}, log ${LOG_DIR}/postgres.log)"
  fi

  wait_tcp "${POSTGRES_HOST}" "${POSTGRES_PORT}" postgres
  if ! psql -v ON_ERROR_STOP=1 -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -c "select 1" >/dev/null 2>&1; then
    createdb -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" "${POSTGRES_DB}"
  fi
}

stop_postgres() {
  if postgres_running; then
    pg_ctl -D "${POSTGRES_DATA_DIR}" stop -m fast >/dev/null
    echo "stopped postgres"
  else
    echo "postgres is not running"
  fi
}

stop_runtime_services() {
  stop_service gatewayd
  stop_service node-tunneld
  stop_service axnoded
  stop_service imagemgr
  stop_service imagefsd
  stop_service tunneld
  stop_service controld
  stop_service storaged
  stop_matching_processes "${ROOT_DIR}/gateway/gatewayd.*go.*run"
  stop_matching_processes "${ROOT_DIR}/runtime/tunneld.*cmd/node-tunneld"
  stop_matching_processes "${ROOT_DIR}/runtime/tunneld.*cmd/tunneld"
  stop_matching_processes "${ROOT_DIR}/runtime/axnoded.*cmd/axnoded"
  stop_matching_processes "${ROOT_DIR}/runtime/volumed.*cmd/volumed"
  stop_matching_processes "${ROOT_DIR}/runtime/imagemgr.*cmd/imagemgr"
  stop_matching_processes "${ROOT_DIR}/control/storaged.*cmd/storaged"
  stop_matching_processes "${ROOT_DIR}/control/controld.*cmd/controld"
  stop_matching_processes "${ROOT_DIR}/target/debug/imagefsd.*serve-chunk"
  stop_matching_processes "127.0.0.1:24000"
  stop_matching_processes "127.0.0.1:24020"
  stop_matching_processes "127.0.0.1:24100"
  stop_matching_processes "127.0.0.1:25080"
  stop_matching_processes "${ROOT_DIR}/.dev/run/axnoded.sock"
  stop_matching_processes "${ROOT_DIR}/.dev/run/volumed.sock"
  stop_matching_processes "${ROOT_DIR}/.dev/run/imagemgr.sock"
  stop_matching_processes "${ROOT_DIR}/.dev/run/imagefsd-chunk.sock"
  stop_matching_processes "${ROOT_DIR}/.dev/imagemgr"
  stop_matching_processes "${ROOT_DIR}/target/debug/imagefsd"
  rm -f \
    "${RUN_DIR}/axnoded.sock" \
    "${RUN_DIR}/volumed.sock" \
    "${RUN_DIR}/imagemgr.sock" \
    "${RUN_DIR}/imagefsd-chunk.sock"
  sleep 1
}

prepare_workspace_config() {
  AXERN_DEV_CONTROL_PLANE_TARGET="${AXERN_DEV_CONTROL_PLANE_TARGET}" \
  AXERN_DEV_CONTROL_PLANE_NODE_ID="${AXERN_DEV_CONTROL_PLANE_NODE_ID}" \
  AXERN_DEV_CONTROL_PLANE_NODE_TARGET="${AXERN_DEV_CONTROL_PLANE_NODE_TARGET}" \
  AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN="${AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN}" \
  bash "${ROOT_DIR}/scripts/devbox/node-dev-prepare.sh"
}

build_runtime_artifacts() {
  make -C "${ROOT_DIR}" imagefsd-build >/dev/null
  CGO_ENABLED=0 go -C "${ROOT_DIR}/runtime/axnoded" build -o "${BIN_DIR}/axnoded-runtime-runner" ./cmd/axnoded-runtime-runner
  CGO_ENABLED=0 go -C "${ROOT_DIR}/runtime/tunneld" build -o "${BIN_DIR}/tunnel-agent" ./cmd/tunnel-agent
}

prepare_workspace() {
  prepare_workspace_config
  build_runtime_artifacts
}

ensure_workspace_prepared() {
  ensure_dirs
  if [ ! -f "${DEV_DIR}/axnoded/config.toml" ] || [ ! -f "${DEV_DIR}/certs/ca.crt" ]; then
    prepare_workspace_config
  fi
}

run_migrations() {
  go -C "${ROOT_DIR}/control/controld" run ./cmd/migrate -postgres-dsn "${POSTGRES_DSN}" up
}

start_storaged() {
  start_service storaged "exec go -C '${ROOT_DIR}/control/storaged' run ./cmd/storaged \
    -grpc-address 127.0.0.1:24020 \
    -http-address 127.0.0.1:24021 \
    -postgres-dsn '${POSTGRES_DSN}'"
  wait_tcp 127.0.0.1 24020 storaged
}

start_controld() {
  start_service controld "exec go -C '${ROOT_DIR}/control/controld' run ./cmd/controld \
    -grpc-address 127.0.0.1:24000 \
    -http-address 127.0.0.1:24001 \
    -heartbeat-freshness-window 15s \
    -summary-freshness-window 15s \
    -tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -tls-cert '${DEV_DIR}/certs/controld.crt' \
    -tls-key '${DEV_DIR}/certs/controld.key' \
    -secrets-master-key '${AXERN_SECRETS_MASTER_KEY}' \
    -storaged-target 127.0.0.1:24020 \
    -function-gateway-url http://127.0.0.1:25080 \
    -function-gateway-token '${AXERN_DEV_TOKEN}' \
    -function-bundle-base-url http://127.0.0.1:24001 \
    -function-bundle-token '${AXERN_DEV_TOKEN}' \
    -tunnel-relays 'default,127.0.0.1:25000,127.0.0.1:24100,1,false' \
    -postgres-dsn '${POSTGRES_DSN}'"
  wait_tcp 127.0.0.1 24000 controld
}

start_tunneld() {
  start_service tunneld "exec go -C '${ROOT_DIR}/runtime/tunneld' run ./cmd/tunneld \
    -listen 127.0.0.1:24100 \
    -control-target 127.0.0.1:24000 \
    -tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -tls-cert '${DEV_DIR}/certs/client.crt' \
    -tls-key '${DEV_DIR}/certs/client.key' \
    -relay-tls-cert '${DEV_DIR}/certs/tunneld.crt' \
    -relay-tls-key '${DEV_DIR}/certs/tunneld.key'"
  wait_tcp 127.0.0.1 24100 tunneld
}

start_imagefsd() {
  start_service imagefsd "exec '${ROOT_DIR}/target/debug/imagefsd' --verbose serve-chunk \
    --chunk-db-dir '${DEV_DIR}/imagefsd/chunkdb' \
    --listen-port 9876 \
    --chunk-server-sock '${RUN_DIR}/imagefsd-chunk.sock' \
    --log-file '${LOG_DIR}/imagefsd-inner.log'"
  wait_unix_socket "${RUN_DIR}/imagefsd-chunk.sock" imagefsd
}

start_imagemgr() {
  start_service imagemgr "exec '${ROOT_DIR}/scripts/devbox/sudo-go.sh' \
    -C '${ROOT_DIR}/runtime/imagemgr' run ./cmd/imagemgr \
    -debug \
    -root '${DEV_DIR}/imagemgr' \
    -node_id 'node-devbox' \
    -imagefsd_bin '${ROOT_DIR}/target/debug/imagefsd' \
    -oss_template '${ROOT_DIR}/runtime/imagemgr/configs/oss_backend.json.example' \
    -nydus_template '${ROOT_DIR}/runtime/imagemgr/configs/nydus_registry.json.example' \
    -oss_auths_path '${ROOT_DIR}/runtime/imagemgr/oss_auths.json.example' \
    -registry_auths_path '${ROOT_DIR}/runtime/imagemgr/registry_auths.json.example' \
    -http_sock '${RUN_DIR}/imagemgr.sock'"
  wait_unix_socket "${RUN_DIR}/imagemgr.sock" imagemgr
}

start_volumed() {
  start_service volumed "exec go -C '${ROOT_DIR}/runtime/volumed' run ./cmd/volumed \
    -root '${DEV_DIR}/volumed' \
    -socket '${RUN_DIR}/volumed.sock' \
    -local-root '${DEV_DIR}/volumed/local'"
  wait_unix_socket "${RUN_DIR}/volumed.sock" volumed
}

start_axnoded() {
  start_service axnoded "exec '${ROOT_DIR}/scripts/devbox/sudo-go.sh' \
    -C '${ROOT_DIR}/runtime/axnoded' run ./cmd/axnoded \
    -root '${DEV_DIR}/axnoded' \
    -config '${DEV_DIR}/axnoded/config.toml' \
    -socket '${RUN_DIR}/axnoded.sock' \
    -grpc-address 127.0.0.1:23000 \
    -http-address 127.0.0.1:23001 \
    -log-level debug \
    -log-file '${LOG_DIR}/axnoded-inner.log'"
  wait_tcp 127.0.0.1 23000 axnoded
  wait_unix_socket "${RUN_DIR}/axnoded.sock" axnoded
}

start_node_tunneld() {
  start_service node-tunneld "exec go -C '${ROOT_DIR}/runtime/tunneld' run ./cmd/node-tunneld \
    -node-id '${AXERN_DEV_CONTROL_PLANE_NODE_ID}' \
    -node-auth-token '${AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN}' \
    -control-target 127.0.0.1:24000 \
    -operator-socket '${RUN_DIR}/axnoded.sock' \
    -tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -tls-cert '${DEV_DIR}/certs/client.crt' \
    -tls-key '${DEV_DIR}/certs/client.key' \
    -relay-tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -runsc-root '${DEV_DIR}/axnoded/root/runsc' \
    -agent-binary '${BIN_DIR}/tunnel-agent'"
}

ensure_gateway_dashboard_assets() {
  if [ -s "${GATEWAY_DASHBOARD_VENDOR_DIR}/xterm.js" ] &&
    [ -s "${GATEWAY_DASHBOARD_VENDOR_DIR}/xterm.css" ] &&
    [ -s "${GATEWAY_DASHBOARD_VENDOR_DIR}/addon-fit.js" ]; then
    return
  fi

  echo "preparing gateway dashboard assets"
  make -C "${ROOT_DIR}" gateway-dashboard-assets
}

start_gatewayd() {
  ensure_gateway_dashboard_assets
  start_service gatewayd "exec go -C '${ROOT_DIR}/gateway/gatewayd' run . \
    -http-address 127.0.0.1:25080 \
    -control-edge-address 127.0.0.1:25000 \
    -control-edge-tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -control-edge-tls-cert '${DEV_DIR}/certs/gatewayd.crt' \
    -control-edge-tls-key '${DEV_DIR}/certs/gatewayd.key' \
    -tunnel-relay-target 127.0.0.1:24100 \
    -tunnel-relay-tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -dashboard-enabled \
    -dashboard-vendor-dir '${GATEWAY_DASHBOARD_VENDOR_DIR}' \
    -control-target 127.0.0.1:24000 \
    -tls-ca-cert '${DEV_DIR}/certs/ca.crt' \
    -tls-cert '${DEV_DIR}/certs/gatewayd.crt' \
    -tls-key '${DEV_DIR}/certs/gatewayd.key' \
    -dev-token '${AXERN_DEV_TOKEN}'"
  wait_tcp 127.0.0.1 25000 gatewayd
  wait_tcp 127.0.0.1 25080 gatewayd
}

start_all() {
  ensure_linux
  ensure_dirs
  stop_runtime_services
  prepare_workspace
  start_postgres
  run_migrations

  start_storaged
  start_controld
  start_tunneld
  start_imagefsd
  start_imagemgr
  start_volumed
  start_axnoded
  start_node_tunneld
  start_gatewayd

  echo "Axern dev stack is running."
  echo "Control HTTP: http://127.0.0.1:24001/healthz"
  echo "Gateway HTTP: http://127.0.0.1:25080/healthz"
  echo "Gateway dashboard: http://127.0.0.1:25080/dashboard?token=${AXERN_DEV_TOKEN}"
  echo "Logs: ${LOG_DIR}"
}

stop_service_deep() {
  local name="$1"
  stop_service "${name}"
  case "${name}" in
    controld)
      stop_matching_processes "127.0.0.1:24000"
      stop_matching_processes "${ROOT_DIR}/control/controld.*cmd/controld"
      ;;
    storaged)
      stop_matching_processes "127.0.0.1:24020"
      stop_matching_processes "${ROOT_DIR}/control/storaged.*cmd/storaged"
      ;;
    tunneld)
      stop_matching_processes "127.0.0.1:24100"
      stop_matching_processes "${ROOT_DIR}/runtime/tunneld.*cmd/tunneld"
      ;;
    imagefsd)
      stop_matching_processes "${ROOT_DIR}/target/debug/imagefsd.*serve-chunk"
      stop_matching_processes "${ROOT_DIR}/.dev/run/imagefsd-chunk.sock"
      ;;
    imagemgr)
      stop_matching_processes "${ROOT_DIR}/runtime/imagemgr.*cmd/imagemgr"
      stop_matching_processes "${ROOT_DIR}/.dev/run/imagemgr.sock"
      stop_matching_processes "${ROOT_DIR}/.dev/imagemgr"
      ;;
    volumed)
      stop_matching_processes "${ROOT_DIR}/runtime/volumed.*cmd/volumed"
      stop_matching_processes "${ROOT_DIR}/.dev/run/volumed.sock"
      stop_matching_processes "${ROOT_DIR}/.dev/volumed"
      ;;
    axnoded)
      stop_matching_processes "${ROOT_DIR}/runtime/axnoded.*cmd/axnoded"
      stop_matching_processes "${ROOT_DIR}/.dev/run/axnoded.sock"
      ;;
    node-tunneld)
      stop_matching_processes "${ROOT_DIR}/runtime/tunneld.*cmd/node-tunneld"
      ;;
    gatewayd)
      stop_matching_processes "127.0.0.1:25080"
      stop_matching_processes "${ROOT_DIR}/gateway/gatewayd.*go.*run"
      ;;
  esac
  sleep 1
}

restart_service() {
  local service
  service="$(normalize_service "$1")"

  ensure_linux
  ensure_workspace_prepared

  case "${service}" in
    postgres)
      stop_runtime_services
      stop_postgres
      start_postgres
      run_migrations
      start_storaged
      start_controld
      start_tunneld
      start_imagefsd
      start_imagemgr
      start_volumed
      start_axnoded
      start_node_tunneld
      start_gatewayd
      ;;
    controld)
      stop_service_deep gatewayd
      stop_service_deep node-tunneld
      stop_service_deep tunneld
      stop_service_deep controld
      start_postgres
      run_migrations
      start_storaged
      start_controld
      start_tunneld
      start_node_tunneld
      start_gatewayd
      ;;
    storaged)
      stop_service_deep gatewayd
      stop_service_deep node-tunneld
      stop_service_deep axnoded
      stop_service_deep controld
      stop_service_deep storaged
      start_postgres
      start_storaged
      start_controld
      start_axnoded
      start_node_tunneld
      start_gatewayd
      ;;
    tunneld)
      stop_service_deep node-tunneld
      stop_service_deep tunneld
      build_runtime_artifacts
      start_tunneld
      start_node_tunneld
      ;;
    imagefsd)
      stop_service_deep node-tunneld
      stop_service_deep axnoded
      stop_service_deep imagemgr
      stop_service_deep imagefsd
      build_runtime_artifacts
      start_imagefsd
      start_imagemgr
      start_axnoded
      start_node_tunneld
      ;;
    imagemgr)
      stop_service_deep node-tunneld
      stop_service_deep axnoded
      stop_service_deep imagemgr
      start_imagemgr
      start_axnoded
      start_node_tunneld
      ;;
    volumed)
      stop_service_deep node-tunneld
      stop_service_deep axnoded
      stop_service_deep volumed
      start_volumed
      start_axnoded
      start_node_tunneld
      ;;
    axnoded)
      stop_service_deep node-tunneld
      stop_service_deep axnoded
      build_runtime_artifacts
      start_axnoded
      start_node_tunneld
      ;;
    node-tunneld)
      build_runtime_artifacts
      stop_service_deep node-tunneld
      start_node_tunneld
      ;;
    gatewayd)
      stop_service_deep gatewayd
      start_gatewayd
      ;;
  esac

  echo "restarted ${service}"
}

status_all() {
  ensure_dirs
  local name
  for name in $(services); do
    if [ "${name}" = "postgres" ]; then
      if postgres_running; then
        echo "postgres running $(service_health postgres running)"
      else
        echo "postgres stopped $(service_health postgres stopped)"
      fi
    elif is_running "${name}"; then
      echo "${name} running pid=$(cat "$(pid_file "${name}")") $(service_health "${name}" running)"
    else
      echo "${name} stopped $(service_health "${name}" stopped)"
    fi
  done
}

down_all() {
  ensure_dirs
  stop_runtime_services
  stop_postgres
}

logs() {
  local service="${1:-}"
  local log_file
  ensure_dirs
  if [ -z "${service}" ]; then
    if ! find "${LOG_DIR}" -maxdepth 1 -type f -name '*.log' -print -quit | grep -q .; then
      echo "no standalone dev stack logs found under ${LOG_DIR}"
      return
    fi
    find "${LOG_DIR}" -maxdepth 1 -type f -name '*.log' -printf '%f\n' | sort
    return
  fi

  if [ "${service}" = "all" ]; then
    if ! find "${LOG_DIR}" -maxdepth 1 -type f -name '*.log' -print -quit | grep -q .; then
      echo "no standalone dev stack logs found under ${LOG_DIR}"
      return
    fi
    tail -n 80 -f "${LOG_DIR}"/*.log
    return
  fi

  service="$(normalize_service "${service}")"
  log_file="${LOG_DIR}/${service}.log"
  if [ ! -f "${log_file}" ]; then
    echo "log file not found for ${service}: ${log_file}" >&2
    echo "available logs:" >&2
    logs >&2
    return 1
  fi

  tail -n 200 -f "${log_file}"
}

reset_all() {
  down_all
  rm -rf "${POSTGRES_DATA_DIR}" "${PID_DIR}" "${BIN_DIR}"
  echo "removed standalone dev stack state under ${STACK_DIR}"
}

command="${1:-}"
case "${command}" in
  up)
    start_all
    ;;
  status)
    status_all
    ;;
  down)
    down_all
    ;;
  postgres-up)
    ensure_linux
    ensure_dirs
    start_postgres
    ;;
  postgres-down)
    ensure_linux
    ensure_dirs
    stop_postgres
    ;;
  migrate)
    ensure_linux
    ensure_dirs
    start_postgres
    run_migrations
    ;;
  restart)
    shift
    restart_service "${1:-}"
    ;;
  logs)
    shift
    logs "${1:-}"
    ;;
  reset)
    reset_all
    ;;
  -h|--help|"")
    usage
    ;;
  *)
    usage
    exit 2
    ;;
esac
