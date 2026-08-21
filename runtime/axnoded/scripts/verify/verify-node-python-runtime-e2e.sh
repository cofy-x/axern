#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

AXNODED_SOCKET="${AXNODED_SOCKET:-/shared/run/axnoded.sock}"
AXNODED_HTTP_ADDRESS="${AXNODED_HTTP_ADDRESS:-0.0.0.0:23001}"
NODE_GRPC_ADDRESS="${NODE_GRPC_ADDRESS:-0.0.0.0:24010}"
NODE_CONTAINER_NAME="${NODE_CONTAINER_NAME:-axnoded-node-python-runtime-e2e}"
CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_ADDRESS:-127.0.0.1:24100}"
CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_ADDRESS:-127.0.0.1:24101}"
GATEWAY_CONTROL_PORT="${GATEWAY_CONTROL_PORT:-25000}"
GATEWAY_HTTP_PORT="${GATEWAY_HTTP_PORT:-25080}"
CONTROL_PLANE_NODE_ID="${CONTROL_PLANE_NODE_ID:-node-python-runtime-e2e}"
CONTROL_PLANE_NODE_AUTH_TOKEN="${CONTROL_PLANE_NODE_AUTH_TOKEN:-node-python-runtime-e2e-token}"
PYTHON_RUNTIME_IMAGE_REF="${PYTHON_RUNTIME_IMAGE_REF:-axern/python311-runtime:dev}"
POSTGRES_CONTAINER_NAME="${POSTGRES_CONTAINER_NAME:-axnoded-python-runtime-e2e-postgres}"
POSTGRES_NETWORK_NAME="${POSTGRES_NETWORK_NAME:-axnoded-python-runtime-e2e-net}"
POSTGRES_DB="${POSTGRES_DB:-axern}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
CONTROLD_POSTGRES_DSN="${CONTROLD_POSTGRES_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_CONTAINER_NAME}:5432/${POSTGRES_DB}?sslmode=disable}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

shared_run_dir="$(mktemp -d)"
cert_dir="$(mktemp -d)"
controld_log="$(mktemp)"
python311_stdout="$(mktemp)"
CONTROLD_CONTAINER_NAME="${CONTROLD_CONTAINER_NAME:-axnoded-python-runtime-e2e-controld}"
GATEWAYD_CONTAINER_NAME="${GATEWAYD_CONTAINER_NAME:-axnoded-python-runtime-e2e-gatewayd}"
STORAGED_CONTAINER_NAME="${STORAGED_CONTAINER_NAME:-axnoded-python-runtime-e2e-storaged}"
STORAGED_GRPC_PORT="${STORAGED_GRPC_PORT:-24020}"
STORAGED_HTTP_PORT="${STORAGED_HTTP_PORT:-24021}"

CONTROLD_GRPC_HOST="${CONTROLD_GRPC_ADDRESS%:*}"
CONTROLD_GRPC_PORT="${CONTROLD_GRPC_ADDRESS##*:}"
CONTROLD_HTTP_HOST="${CONTROLD_HTTP_ADDRESS%:*}"
CONTROLD_HTTP_PORT="${CONTROLD_HTTP_ADDRESS##*:}"
NODE_GRPC_HOST="${NODE_GRPC_ADDRESS%:*}"
NODE_GRPC_PORT="${NODE_GRPC_ADDRESS##*:}"

CONTROLD_GRPC_PORT="$(reserve_unique_host_port "${CONTROLD_GRPC_HOST}" 0)"
CONTROLD_GRPC_ADDRESS="${CONTROLD_GRPC_HOST}:${CONTROLD_GRPC_PORT}"
CONTROLD_HTTP_PORT="$(reserve_unique_host_port "${CONTROLD_HTTP_HOST}" 0 "${CONTROLD_GRPC_PORT}")"
CONTROLD_HTTP_ADDRESS="${CONTROLD_HTTP_HOST}:${CONTROLD_HTTP_PORT}"
NODE_GRPC_PORT="$(reserve_unique_host_port "${NODE_GRPC_HOST}" 0 "${CONTROLD_GRPC_PORT}" "${CONTROLD_HTTP_PORT}")"
NODE_GRPC_ADDRESS="${NODE_GRPC_HOST}:${NODE_GRPC_PORT}"

dump_logs() {
  echo "--- controld log ---" >&2
  docker logs "${CONTROLD_CONTAINER_NAME}" >&2 || cat "${controld_log}" >&2 || true
  echo "--- storaged log ---" >&2
  docker logs "${STORAGED_CONTAINER_NAME}" >&2 || true
  echo "--- gatewayd log ---" >&2
  docker logs "${GATEWAYD_CONTAINER_NAME}" >&2 || true
  echo "--- controld /nodesz ---" >&2
  curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" >&2 || true
  echo >&2
  echo "--- node container logs ---" >&2
  docker logs "${NODE_CONTAINER_NAME}" >&2 || true
  echo "--- axnoded log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 120 /var/log/axnoded/axnoded.log >&2 || true
  echo "--- imagemgr log tail ---" >&2
  docker exec "${NODE_CONTAINER_NAME}" tail -n 120 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
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

cleanup() {
  docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${STORAGED_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${CONTROLD_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${GATEWAYD_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true
  rm -rf "${shared_run_dir}" "${cert_dir}" "${controld_log}" "${python311_stdout}"
}
trap cleanup EXIT

ensure_verify_image
docker rm -f "${CONTROLD_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker rm -f "${GATEWAYD_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker rm -f "${STORAGED_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true

docker network create "${POSTGRES_NETWORK_NAME}" >/dev/null

docker run -d \
  --name "${POSTGRES_CONTAINER_NAME}" \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
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
docker run --rm "${PYTHON_RUNTIME_IMAGE_REF}" python --version >"${python311_stdout}"
grep -q '^Python 3\.11\.' "${python311_stdout}"
docker run --rm "${PYTHON_RUNTIME_IMAGE_REF}" /bin/sh -lc 'python -m pip --version >/dev/null'

docker run --rm \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  "${IMAGE_TAG}" \
  /usr/local/bin/controld-migrate \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    up

docker run --rm \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  --volume "${cert_dir}:/shared/certs:ro" \
  "${IMAGE_TAG}" \
  /usr/local/bin/controld-access-bootstrap \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    -principal-name local-admin \
    -display-name "Local Administrator" \
    -credential-label local-client \
    -certificate /shared/certs/client.crt \
    -rollout-worker-certificate /shared/certs/rollout-worker.crt

docker run -d \
  --name "${STORAGED_CONTAINER_NAME}" \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  "${IMAGE_TAG}" \
  /usr/local/bin/storaged \
    -grpc-address "0.0.0.0:${STORAGED_GRPC_PORT}" \
    -http-address "0.0.0.0:${STORAGED_HTTP_PORT}" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" >/dev/null

deadline=$((SECONDS + 60))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if docker exec "${STORAGED_CONTAINER_NAME}" curl -fsS "http://127.0.0.1:${STORAGED_HTTP_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! docker exec "${STORAGED_CONTAINER_NAME}" curl -fsS "http://127.0.0.1:${STORAGED_HTTP_PORT}/healthz" >/dev/null 2>&1; then
  echo "storaged did not become ready in time" >&2
  dump_logs
  exit 1
fi

docker run -d \
  --name "${CONTROLD_CONTAINER_NAME}" \
  --network "${POSTGRES_NETWORK_NAME}" \
  --network-alias controld \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -p "${CONTROLD_GRPC_HOST}:${CONTROLD_GRPC_PORT}:${CONTROLD_GRPC_PORT}" \
  -p "${CONTROLD_HTTP_HOST}:${CONTROLD_HTTP_PORT}:${CONTROLD_HTTP_PORT}" \
  --volume "${cert_dir}:/shared/certs:ro" \
  -e "AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE=${PYTHON_RUNTIME_IMAGE_REF}" \
  "${IMAGE_TAG}" \
  /usr/local/bin/controld \
    -grpc-address "0.0.0.0:${CONTROLD_GRPC_PORT}" \
    -http-address "0.0.0.0:${CONTROLD_HTTP_PORT}" \
    -tls-ca-cert /shared/certs/ca.crt \
    -tls-cert /shared/certs/controld.crt \
    -tls-key /shared/certs/controld.key \
    -secrets-master-key "test-only-master-key-32-bytes!!!" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    -storaged-target "${STORAGED_CONTAINER_NAME}:${STORAGED_GRPC_PORT}" \
    -log-level info >"${controld_log}" 2>&1

deadline=$((SECONDS + 60))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
  echo "controld did not become ready in time" >&2
  dump_logs
  exit 1
fi

docker run -d \
  --name "${GATEWAYD_CONTAINER_NAME}" \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  --volume "${cert_dir}:/shared/certs:ro" \
  "${IMAGE_TAG}" \
  /usr/local/bin/gatewayd \
    -control-target "controld:${CONTROLD_GRPC_PORT}" \
    -control-edge-address "0.0.0.0:${GATEWAY_CONTROL_PORT}" \
    -control-edge-tls-ca-cert /shared/certs/ca.crt \
    -control-edge-tls-cert /shared/certs/gatewayd.crt \
    -control-edge-tls-key /shared/certs/gatewayd.key \
    -http-address "0.0.0.0:${GATEWAY_HTTP_PORT}" \
    -tls-ca-cert /shared/certs/ca.crt \
    -tls-cert /shared/certs/gatewayd.crt \
    -tls-key /shared/certs/gatewayd.key \
    -log-level info >/dev/null

deadline=$((SECONDS + 60))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if docker exec "${GATEWAYD_CONTAINER_NAME}" curl -fsS "http://127.0.0.1:${GATEWAY_HTTP_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! docker exec "${GATEWAYD_CONTAINER_NAME}" curl -fsS "http://127.0.0.1:${GATEWAY_HTTP_PORT}/healthz" >/dev/null 2>&1; then
  echo "gatewayd did not become ready in time" >&2
  dump_logs
  exit 1
fi

docker run -d \
  --name "${NODE_CONTAINER_NAME}" \
  --privileged \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  -p "${NODE_GRPC_HOST}:${NODE_GRPC_PORT}:${NODE_GRPC_PORT}" \
  --volume "${shared_run_dir}:/shared/run" \
  --volume "${cert_dir}:/shared/certs:ro" \
  -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
  -e "AXNODED_GRPC_ADDRESS=0.0.0.0:${NODE_GRPC_PORT}" \
  -e "REGISTRY_PROXY_URL=${REGISTRY_PROXY_URL}" \
  -e "REGISTRY_NO_PROXY=${REGISTRY_NO_PROXY}" \
  -e "AXNODED_HTTP_ADDRESS=${AXNODED_HTTP_ADDRESS}" \
  -e "AXNODED_CONTROL_PLANE_TARGET=host.docker.internal:${CONTROLD_GRPC_PORT}" \
  -e "AXNODED_CONTROL_PLANE_NODE_ID=${CONTROL_PLANE_NODE_ID}" \
  -e "AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN=${CONTROL_PLANE_NODE_AUTH_TOKEN}" \
  -e "AXNODED_CONTROL_PLANE_NODE_TARGET=host.docker.internal:${NODE_GRPC_PORT}" \
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
  if [ -S "${shared_run_dir}/axnoded.sock" ] && \
    docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
    break
  fi
  sleep 2
done

if ! [ -S "${shared_run_dir}/axnoded.sock" ]; then
  echo "axnoded socket was not exposed to the host" >&2
  dump_logs
  exit 1
fi
docker exec "${NODE_CONTAINER_NAME}" chmod 666 "${AXNODED_SOCKET}" >/dev/null 2>&1 || true

if ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
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

if ! docker run --rm \
  --network "${POSTGRES_NETWORK_NAME}" \
  --platform "${VERIFY_DOCKER_PLATFORM}" \
  --volume "${cert_dir}:/shared/certs:ro" \
  --volume "${REPO_ROOT}/sdk/python/tests/e2e/python_runtime_e2e.py:/tmp/python_runtime_e2e.py:ro" \
  -e AXERN_TLS_CA_CERT=/shared/certs/ca.crt \
  -e AXERN_TLS_CERT=/shared/certs/client.crt \
  -e AXERN_TLS_KEY=/shared/certs/client.key \
  -e AXERN_TLS_SERVER_NAME=gatewayd \
  -e AXERN_PROXY_MODE=direct \
  "${PYTHON_RUNTIME_IMAGE_REF}" \
  python /tmp/python_runtime_e2e.py \
    --endpoint "${GATEWAYD_CONTAINER_NAME}:${GATEWAY_CONTROL_PORT}" \
    --runtime-id python311 \
    --expected-image-ref "${PYTHON_RUNTIME_IMAGE_REF}"; then
  dump_logs
  exit 1
fi

echo "verify_node_python_runtime_e2e_client_ok=true"
