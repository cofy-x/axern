#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
POSTGRES_TEST_IMAGE=${POSTGRES_TEST_IMAGE:-postgres:16-alpine}
container_name=""

cleanup() {
  if [[ -n "${container_name}" ]]; then
    docker rm -f "${container_name}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

start_output=""
for attempt in 1 2 3; do
  container_name="axern-controld-postgres-test-$$-${attempt}"
  if start_output=$(docker run -d --name "${container_name}" \
    -e POSTGRES_PASSWORD=axern \
    -e POSTGRES_DB=axern \
    -p 127.0.0.1::5432 \
    "${POSTGRES_TEST_IMAGE}" 2>&1); then
    break
  fi
  echo "PostgreSQL test container start attempt ${attempt} failed: ${start_output}" >&2
  cleanup
  container_name=""
  sleep 1
done

if [[ -z "${container_name}" ]]; then
  echo "failed to start PostgreSQL test container after 3 attempts" >&2
  exit 1
fi

ready=false
for _ in $(seq 1 120); do
  if docker exec "${container_name}" pg_isready -U postgres -d axern >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! docker inspect -f '{{.State.Running}}' "${container_name}" 2>/dev/null | grep -qx true; then
    break
  fi
  sleep 0.25
done

if [[ "${ready}" != true ]]; then
  echo "PostgreSQL test container did not become ready" >&2
  docker logs "${container_name}" >&2 || true
  exit 1
fi

port=$(docker port "${container_name}" 5432/tcp | awk -F: 'NR == 1 {print $NF}')
if [[ -z "${port}" ]]; then
  echo "failed to resolve ephemeral PostgreSQL test port" >&2
  exit 1
fi

AXERN_TEST_POSTGRES_DSN="postgres://postgres:axern@127.0.0.1:${port}/axern?sslmode=disable" \
  make -C "${ROOT}/control/controld" test
