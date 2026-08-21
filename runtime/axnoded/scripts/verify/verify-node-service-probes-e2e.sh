#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

AXERN_BIN="${AXERN_BIN:-${REPO_ROOT}/bin/axern}"
CONTROLD_BIN="${CONTROLD_BIN:-${REPO_ROOT}/bin/controld}"
CONTROLD_MIGRATE_BIN="${CONTROLD_MIGRATE_BIN:-${REPO_ROOT}/bin/controld-migrate}"
CONTROLD_ACCESS_BOOTSTRAP_BIN="${CONTROLD_ACCESS_BOOTSTRAP_BIN:-${REPO_ROOT}/bin/controld-access-bootstrap}"
STORAGED_BIN="${STORAGED_BIN:-${REPO_ROOT}/bin/storaged}"
GATEWAYD_BIN="${GATEWAYD_BIN:-${REPO_ROOT}/bin/gatewayd}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/shared/run/axnoded.sock}"
NODE_GRPC_ADDRESS="${NODE_GRPC_ADDRESS:-127.0.0.1:24010}"
NODE_HTTP_ADDRESS="${NODE_HTTP_ADDRESS:-0.0.0.0:23001}"
CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_ADDRESS:-127.0.0.1:24100}"
CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_ADDRESS:-127.0.0.1:24101}"
STORAGED_GRPC_ADDRESS="${STORAGED_GRPC_ADDRESS:-127.0.0.1:24020}"
STORAGED_HTTP_ADDRESS="${STORAGED_HTTP_ADDRESS:-127.0.0.1:24021}"
GATEWAY_CONTROL_ADDRESS="${GATEWAY_CONTROL_ADDRESS:-127.0.0.1:25000}"
GATEWAY_HTTP_ADDRESS="${GATEWAY_HTTP_ADDRESS:-127.0.0.1:25080}"
CONTROL_PLANE_NODE_ID="${CONTROL_PLANE_NODE_ID:-node-service-probes-e2e}"
CONTROL_PLANE_NODE_AUTH_TOKEN="${CONTROL_PLANE_NODE_AUTH_TOKEN:-node-service-probes-e2e-token}"
POSTGRES_CONTAINER_NAME="${POSTGRES_CONTAINER_NAME:-axnoded-service-probes-e2e-postgres}"
POSTGRES_NETWORK_NAME="${POSTGRES_NETWORK_NAME:-axnoded-service-probes-e2e-net}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axnoded-service-probes-e2e-node}"
POSTGRES_DB="${POSTGRES_DB:-axern}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_HOST_PORT="${POSTGRES_HOST_PORT:-25433}"
CONTROLD_POSTGRES_DSN="${CONTROLD_POSTGRES_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_HOST_PORT}/${POSTGRES_DB}?sslmode=disable}"
PYTHON_RUNTIME_IMAGE_REF="${PYTHON_RUNTIME_IMAGE_REF:-axern/python311-runtime:dev}"
GO="${GO:-go}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

shared_run_dir="$(mktemp -d)"
cert_dir="$(mktemp -d)"
controld_log="$(mktemp)"
storaged_log="$(mktemp)"
gatewayd_log="$(mktemp)"
cli_config_dir="$(mktemp -d)"
cli_config_file="${cli_config_dir}/config.json"
CONTROLD_PID=""
STORAGED_PID=""
GATEWAYD_PID=""

# Keep CLI calls isolated from the operator's selected Axern context.
export AXERN_CONFIG="${cli_config_file}"
unset AXERN_CONTEXT
unset AXERN_ENDPOINT
unset AXERN_SERVICE_URL
unset AXERN_GATEWAY_SSH_TARGET
unset AXERN_GATEWAY_SSH_KEY

CONTROLD_GRPC_HOST="${CONTROLD_GRPC_ADDRESS%:*}"
CONTROLD_GRPC_PORT="${CONTROLD_GRPC_ADDRESS##*:}"
CONTROLD_HTTP_HOST="${CONTROLD_HTTP_ADDRESS%:*}"
CONTROLD_HTTP_PORT="${CONTROLD_HTTP_ADDRESS##*:}"
STORAGED_GRPC_HOST="${STORAGED_GRPC_ADDRESS%:*}"
STORAGED_GRPC_PORT="${STORAGED_GRPC_ADDRESS##*:}"
STORAGED_HTTP_HOST="${STORAGED_HTTP_ADDRESS%:*}"
STORAGED_HTTP_PORT="${STORAGED_HTTP_ADDRESS##*:}"
NODE_GRPC_HOST="${NODE_GRPC_ADDRESS%:*}"
NODE_GRPC_PORT="${NODE_GRPC_ADDRESS##*:}"
GATEWAY_CONTROL_HOST="${GATEWAY_CONTROL_ADDRESS%:*}"
GATEWAY_HTTP_HOST="${GATEWAY_HTTP_ADDRESS%:*}"

CONTROLD_GRPC_PORT="$(reserve_unique_host_port "${CONTROLD_GRPC_HOST}" 0)"
CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_HOST}:${CONTROLD_GRPC_PORT}"
CONTROLD_HTTP_PORT="$(reserve_unique_host_port "${CONTROLD_HTTP_HOST}" 0 "${CONTROLD_GRPC_PORT}")"
CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_HOST}:${CONTROLD_HTTP_PORT}"
STORAGED_GRPC_PORT="$(reserve_unique_host_port "${STORAGED_GRPC_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}")"
STORAGED_GRPC_ADDRESS="${STORAGED_GRPC_HOST}:${STORAGED_GRPC_PORT}"
STORAGED_HTTP_PORT="$(reserve_unique_host_port "${STORAGED_HTTP_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${STORAGED_GRPC_PORT}")"
STORAGED_HTTP_ADDRESS="${STORAGED_HTTP_HOST}:${STORAGED_HTTP_PORT}"
NODE_GRPC_PORT="$(reserve_unique_host_port "${NODE_GRPC_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${STORAGED_GRPC_PORT}" "${STORAGED_HTTP_PORT}")"
NODE_GRPC_ADDRESS="${NODE_GRPC_HOST}:${NODE_GRPC_PORT}"
GATEWAY_CONTROL_PORT="$(reserve_unique_host_port "${GATEWAY_CONTROL_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${STORAGED_GRPC_PORT}" "${STORAGED_HTTP_PORT}" "${NODE_GRPC_PORT}")"
GATEWAY_CONTROL_ADDRESS="${GATEWAY_CONTROL_HOST}:${GATEWAY_CONTROL_PORT}"
GATEWAY_HTTP_PORT="$(reserve_unique_host_port "${GATEWAY_HTTP_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${STORAGED_GRPC_PORT}" "${STORAGED_HTTP_PORT}" "${NODE_GRPC_PORT}" "${GATEWAY_CONTROL_PORT}")"
GATEWAY_HTTP_ADDRESS="${GATEWAY_HTTP_HOST}:${GATEWAY_HTTP_PORT}"
POSTGRES_HOST_PORT="$(reserve_unique_host_port "127.0.0.1" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}" "${STORAGED_GRPC_PORT}" "${STORAGED_HTTP_PORT}" "${NODE_GRPC_PORT}" "${GATEWAY_CONTROL_PORT}" "${GATEWAY_HTTP_PORT}")"
CONTROLD_POSTGRES_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_HOST_PORT}/${POSTGRES_DB}?sslmode=disable"

cleanup() {
  if [ -n "${GATEWAYD_PID}" ]; then
    kill "${GATEWAYD_PID}" >/dev/null 2>&1 || true
    wait "${GATEWAYD_PID}" >/dev/null 2>&1 || true
  fi
  if [ -n "${CONTROLD_PID}" ]; then
    kill "${CONTROLD_PID}" >/dev/null 2>&1 || true
    wait "${CONTROLD_PID}" >/dev/null 2>&1 || true
  fi
  if [ -n "${STORAGED_PID}" ]; then
    kill "${STORAGED_PID}" >/dev/null 2>&1 || true
    wait "${STORAGED_PID}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true
  rm -rf "${shared_run_dir}" "${cert_dir}" "${controld_log}" "${storaged_log}" "${gatewayd_log}" "${cli_config_dir}"
}
trap cleanup EXIT

dump_logs() {
  echo "--- controld log ---" >&2
  cat "${controld_log}" >&2 || true
  echo "--- storaged log ---" >&2
  cat "${storaged_log}" >&2 || true
  echo "--- gatewayd log ---" >&2
  cat "${gatewayd_log}" >&2 || true
  echo "--- controld /nodesz ---" >&2
  curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" >&2 || true
  echo >&2
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 200 /var/log/axnoded/axnoded.log >&2 || true
}

wait_for_postgres() {
  local deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if docker exec "${POSTGRES_CONTAINER_NAME}" pg_isready -U "${POSTGRES_USER}" >/dev/null 2>&1 &&
      docker exec "${POSTGRES_CONTAINER_NAME}" psql -U "${POSTGRES_USER}" -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${POSTGRES_DB}'" 2>/dev/null | grep -qx '1'; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_for_json_field() {
  local cmd="$1"
  local python_expr="$2"
  local timeout="${3:-60}"
  local deadline=$((SECONDS + timeout))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local payload
    if payload="$(eval "${cmd}" 2>/dev/null)"; then
      if python3 -c "import json,sys; data=json.load(sys.stdin); sys.exit(0 if (${python_expr}) else 1)" <<<"${payload}"; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

wait_for_http_ready() {
  local url="$1"
  local timeout="${2:-60}"
  local deadline=$((SECONDS + timeout))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

build_binary() {
  local path="$1"
  local target="$2"
  (cd "${REPO_ROOT}" && "${GO}" build -o "${path}" "${target}")
}

ensure_verify_image
build_binary "${AXERN_BIN}" ./apps/cli
build_binary "${CONTROLD_BIN}" ./control/controld/cmd/controld
build_binary "${CONTROLD_MIGRATE_BIN}" ./control/controld/cmd/migrate
build_binary "${CONTROLD_ACCESS_BOOTSTRAP_BIN}" ./control/controld/cmd/access-bootstrap
build_binary "${STORAGED_BIN}" ./control/storaged/cmd/storaged
build_binary "${GATEWAYD_BIN}" ./gateway/gatewayd

docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true
docker network create "${POSTGRES_NETWORK_NAME}" >/dev/null

docker run -d \
  --name "${POSTGRES_CONTAINER_NAME}" \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -p "127.0.0.1:${POSTGRES_HOST_PORT}:5432" \
  -e "POSTGRES_DB=${POSTGRES_DB}" \
  -e "POSTGRES_USER=${POSTGRES_USER}" \
  -e "POSTGRES_PASSWORD=${POSTGRES_PASSWORD}" \
  postgres:16-alpine >/dev/null

if ! wait_for_postgres; then
  echo "postgres did not become ready in time" >&2
  docker logs "${POSTGRES_CONTAINER_NAME}" >&2 || true
  exit 1
fi

IMAGE_REF="${PYTHON_RUNTIME_IMAGE_REF}" bash "${ROOT_DIR}/scripts/runtime/build-python311-runtime-image.sh" >/dev/null
bash "${REPO_ROOT}/scripts/dev-mtls-certs.sh" "${cert_dir}" >/dev/null

export AXERN_TLS_CA_CERT="${cert_dir}/ca.crt"
export AXERN_TLS_CERT="${cert_dir}/client.crt"
export AXERN_TLS_KEY="${cert_dir}/client.key"

"${CONTROLD_MIGRATE_BIN}" \
  -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
  up

"${CONTROLD_ACCESS_BOOTSTRAP_BIN}" \
  -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
  -principal-name local-admin \
  -display-name "Local Administrator" \
  -credential-label local-client \
  -certificate "${cert_dir}/client.crt" \
  -rollout-worker-certificate "${cert_dir}/rollout-worker.crt"

"${STORAGED_BIN}" \
  -grpc-address "${STORAGED_GRPC_ADDRESS}" \
  -http-address "${STORAGED_HTTP_ADDRESS}" \
  -postgres-dsn "${CONTROLD_POSTGRES_DSN}" >"${storaged_log}" 2>&1 &
STORAGED_PID=$!

if ! wait_for_http_ready "http://${STORAGED_HTTP_ADDRESS}/healthz" 60; then
  echo "storaged did not become ready in time" >&2
  dump_logs
  exit 1
fi

AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE="${PYTHON_RUNTIME_IMAGE_REF}" \
  "${CONTROLD_BIN}" \
  -grpc-address "${CONTROLD_GRPC_ADDRESS}" \
  -http-address "${CONTROLD_HTTP_ADDRESS}" \
  -tls-ca-cert "${cert_dir}/ca.crt" \
  -tls-cert "${cert_dir}/controld.crt" \
  -tls-key "${cert_dir}/controld.key" \
  -secrets-master-key "test-only-master-key-32-bytes!!!" \
  -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
  -storaged-target "${STORAGED_GRPC_ADDRESS}" \
  -log-level info >"${controld_log}" 2>&1 &
CONTROLD_PID=$!

if ! wait_for_http_ready "http://${CONTROLD_HTTP_ADDRESS}/healthz" 60; then
  echo "controld did not become ready in time" >&2
  dump_logs
  exit 1
fi

"${GATEWAYD_BIN}" \
  -control-target "${CONTROLD_GRPC_ADDRESS}" \
  -control-edge-address "${GATEWAY_CONTROL_ADDRESS}" \
  -control-edge-tls-ca-cert "${cert_dir}/ca.crt" \
  -control-edge-tls-cert "${cert_dir}/gatewayd.crt" \
  -control-edge-tls-key "${cert_dir}/gatewayd.key" \
  -http-address "${GATEWAY_HTTP_ADDRESS}" \
  -tls-ca-cert "${cert_dir}/ca.crt" \
  -tls-cert "${cert_dir}/gatewayd.crt" \
  -tls-key "${cert_dir}/gatewayd.key" \
  -log-level info >"${gatewayd_log}" 2>&1 &
GATEWAYD_PID=$!

if ! wait_for_http_ready "http://${GATEWAY_HTTP_ADDRESS}/healthz" 60; then
  echo "gatewayd did not become ready in time" >&2
  dump_logs
  exit 1
fi

docker run -d \
  --name "${NODE_CONTAINER_NAME}" \
  --privileged \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -p "${NODE_GRPC_ADDRESS}:${NODE_GRPC_ADDRESS##*:}" \
  --volume "${shared_run_dir}:/shared/run" \
  --volume "${cert_dir}:/shared/certs:ro" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "AXNODED_GRPC_ADDRESS=0.0.0.0:${NODE_GRPC_ADDRESS##*:}" \
  -e "REGISTRY_PROXY_URL=${REGISTRY_PROXY_URL}" \
  -e "REGISTRY_NO_PROXY=${REGISTRY_NO_PROXY}" \
  -e "AXNODED_HTTP_ADDRESS=${NODE_HTTP_ADDRESS}" \
  -e "AXNODED_CONTROL_PLANE_TARGET=host.docker.internal:${CONTROLD_GRPC_ADDRESS##*:}" \
  -e "AXNODED_CONTROL_PLANE_NODE_ID=${CONTROL_PLANE_NODE_ID}" \
  -e "AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN=${CONTROL_PLANE_NODE_AUTH_TOKEN}" \
  -e "AXNODED_CONTROL_PLANE_NODE_TARGET=${NODE_GRPC_ADDRESS}" \
  -e "AXNODED_CONTROL_PLANE_HEARTBEAT_INTERVAL=1s" \
  -e "AXNODED_CONTROL_PLANE_TLS_CA_CERT=/shared/certs/ca.crt" \
  -e "AXNODED_CONTROL_PLANE_TLS_CERT=/shared/certs/node.crt" \
  -e "AXNODED_CONTROL_PLANE_TLS_KEY=/shared/certs/node.key" \
  "${IMAGE_TAG}" \
  /bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >/dev/null

deadline=$((SECONDS + 180))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if ! docker inspect -f '{{.State.Running}}' "${NODE_CONTAINER_NAME}" 2>/dev/null | grep -qx true; then
    echo "node container exited before becoming ready" >&2
    dump_logs
    exit 1
  fi
  if [ -S "${shared_run_dir}/axnoded.sock" ] && docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
    break
  fi
  sleep 2
done

if ! [ -S "${shared_run_dir}/axnoded.sock" ] || ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
  echo "node container did not become ready in time" >&2
  dump_logs
  exit 1
fi

import_oci_image_to_node "${PYTHON_RUNTIME_IMAGE_REF}" "${NODE_CONTAINER_NAME}"

deadline=$((SECONDS + 60))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  nodes_body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" || true)"
  if node_summary_fresh "${CONTROL_PLANE_NODE_ID}" "${nodes_body}"; then
    break
  fi
  sleep 1
done

nodes_body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" || true)"
if ! node_summary_fresh "${CONTROL_PLANE_NODE_ID}" "${nodes_body}"; then
  echo "controld did not observe a fresh node summary in time" >&2
  dump_logs
  exit 1
fi

service_script="$(cat <<'PY'
import http.server, socketserver, threading, time
state = {"ready": False, "live": True}
def flip():
    time.sleep(3)
    state["ready"] = True
    time.sleep(9)
    state["live"] = False
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/readyz":
            code = 200 if state["ready"] else 503
            body = b"ready" if state["ready"] else b"not-ready"
        elif self.path == "/livez":
            code = 200 if state["live"] else 500
            body = b"live" if state["live"] else b"dead"
        else:
            code = 200
            body = b"ok"
        self.send_response(code)
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, format, *args):
        return
threading.Thread(target=flip, daemon=True).start()
socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("0.0.0.0", 8080), Handler) as server:
    server.serve_forever()
PY
)"

env_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create --output json --template-id python311)"
environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_create_output}")"
[ -n "${environment_id}" ] || {
  echo "failed to create environment" >&2
  dump_logs
  exit 1
}

service_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create --output json \
  --environment-id "${environment_id}" \
  --replicas 1 \
  --argv python --argv -u --argv -c --argv "${service_script}" \
  --readiness-http-port 8080 --readiness-http-path /readyz --readiness-period 1s --readiness-timeout 1s \
  --liveness-http-port 8080 --liveness-http-path /livez --liveness-period 1s --liveness-timeout 1s --liveness-failure-threshold 2)"
service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_create_output}")"
[ -n "${service_id}" ] || {
  echo "failed to create service" >&2
  dump_logs
  exit 1
}

get_service_json="\"${AXERN_BIN}\" --endpoint \"${GATEWAY_CONTROL_ADDRESS}\" service get --output json \"${service_id}\""
get_events_json="\"${AXERN_BIN}\" --endpoint \"${GATEWAY_CONTROL_ADDRESS}\" service events --output json \"${service_id}\""
get_replicas_json="\"${AXERN_BIN}\" --endpoint \"${GATEWAY_CONTROL_ADDRESS}\" service replicas --view all --output json \"${service_id}\""

if ! wait_for_json_field "${get_service_json}" "data['service']['status'] == 'ready'" 60; then
  echo "service did not become READY" >&2
  dump_logs
  exit 1
fi

if ! wait_for_json_field "${get_events_json}" "any(e.get('type') == 'liveness-failed' for e in data.get('events', []))" 90; then
  echo "liveness failure event did not appear" >&2
  dump_logs
  exit 1
fi

if ! wait_for_json_field "${get_replicas_json}" "any(r.get('ended') and r.get('diagnostic_code') == 'liveness-probe-failed' for r in data.get('replicas', []))" 60; then
  echo "terminal liveness-failed replica did not appear" >&2
  dump_logs
  exit 1
fi

if ! wait_for_json_field "${get_events_json}" "any(e.get('type') == 'replacement-ready' for e in data.get('events', [])) and any(e.get('type') == 'service-recovered' for e in data.get('events', []))" 90; then
  echo "replacement ready / recovery events did not appear" >&2
  dump_logs
  exit 1
fi

if ! wait_for_json_field "${get_service_json}" "data['service']['status'] == 'ready' and data['service']['ready_replicas'] == 1" 60; then
  echo "service did not return to READY" >&2
  dump_logs
  exit 1
fi

"${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${service_id}" >/dev/null
"${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service replicas "${service_id}" >/dev/null
"${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service events "${service_id}" >/dev/null

echo "verify_node_service_probes_e2e_host_ok=true"
