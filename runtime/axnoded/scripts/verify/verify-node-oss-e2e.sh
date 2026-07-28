#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

COMPOSE_FILE="${ROOT_DIR}/docker/verify/docker-compose.oss-e2e.yml"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-axnoded-node-oss-e2e}"
VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

compose() {
  docker compose -f "${COMPOSE_FILE}" -p "${COMPOSE_PROJECT_NAME}" "$@"
}

dump_logs() {
  echo "--- compose logs ---" >&2
  compose logs --no-color >&2 || true
  echo "--- axnoded log tail ---" >&2
  compose exec -T node tail -n 120 /var/log/axnoded/axnoded.log >&2 || true
  echo "--- imagemgr log tail ---" >&2
  compose exec -T node tail -n 120 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
  echo "--- imagefsd daemon log tails ---" >&2
  compose exec -T node /bin/bash -lc \
    'find /var/lib/imagemgr/daemons -type f -name daemon.log -print -exec tail -n 120 {} \;' >&2 || true
}

cleanup() {
  compose down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_verify_image
cleanup

export IMAGE_TAG
compose up -d oss node

node_container_id="$(compose ps -q node)"
if [ -z "${node_container_id}" ]; then
  echo "failed to resolve node container id" >&2
  exit 1
fi

deadline=$((SECONDS + 180))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}starting{{end}}' "${node_container_id}")"
  if [ "${health}" = "healthy" ] && compose exec -T node curl -fsS http://oss:9000/minio/health/live >/dev/null 2>&1; then
    break
  fi
  if [ "${health}" = "unhealthy" ]; then
    echo "node service became unhealthy" >&2
    dump_logs
    exit 1
  fi
  sleep 2
done

if [ "${health:-starting}" != "healthy" ]; then
  echo "node service did not become healthy in time" >&2
  dump_logs
  exit 1
fi

if ! compose exec -T node /bin/bash /workspace/scripts/verify/verify-node-oss-e2e-in-container.sh; then
  dump_logs
  exit 1
fi

echo "verify_node_oss_e2e_host_ok=true"
