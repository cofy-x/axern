#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-axern/server-base-runtime:dev}"
PLATFORM="${PLATFORM:-}"
CONTAINER_NAME="${CONTAINER_NAME:-axern-server-base-runtime-smoke}"

passed=0
failed=0

pass() { printf '  PASS: %s\n' "$1"; passed=$((passed + 1)); }
fail() { printf '  FAIL: %s\n' "$1"; failed=$((failed + 1)); }
check() {
  if eval "$2"; then
    pass "$1"
  else
    fail "$1"
  fi
}

cleanup() {
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "=== Axern Server Base Runtime Smoke Test ==="
echo "Image: ${IMAGE}"
echo ""

docker image inspect "${IMAGE}" >/dev/null 2>&1 || {
  echo "missing image ${IMAGE}. Build it first with make axnoded-build-server-base-runtime-image." >&2
  exit 1
}

cleanup
run_args=(-d --name "${CONTAINER_NAME}")
if [ -n "${PLATFORM}" ]; then
  run_args+=(--platform "${PLATFORM}")
fi
docker run "${run_args[@]}" "${IMAGE}" >/dev/null

echo "Waiting for container to be ready..."
for i in $(seq 1 30); do
  if docker exec "${CONTAINER_NAME}" pgrep -x sshd >/dev/null 2>&1; then
    break
  fi
  if [ "${i}" -ge 30 ]; then
    echo "ERROR: container did not become ready" >&2
    exit 1
  fi
  sleep 1
done

echo ""
echo "--- Process Checks ---"
check "supervisord is running" \
  "docker exec ${CONTAINER_NAME} pgrep -x supervisord >/dev/null"
check "sshd is running" \
  "docker exec ${CONTAINER_NAME} pgrep -x sshd >/dev/null"

echo ""
echo "--- User Checks ---"
check "axern user exists" \
  "docker exec ${CONTAINER_NAME} id axern >/dev/null 2>&1"
check "axern has passwordless sudo" \
  "docker exec ${CONTAINER_NAME} sudo -u axern sudo -n true 2>/dev/null"

echo ""
echo "--- Runtime Checks ---"
check "node is not installed" \
  "! docker exec ${CONTAINER_NAME} bash -lc 'command -v node' >/dev/null 2>&1"
check "go is not installed" \
  "! docker exec ${CONTAINER_NAME} bash -lc 'command -v go' >/dev/null 2>&1"
check "poetry is not installed" \
  "! docker exec ${CONTAINER_NAME} bash -lc 'command -v poetry' >/dev/null 2>&1"
check "pipx is not installed" \
  "! docker exec ${CONTAINER_NAME} bash -lc 'command -v pipx' >/dev/null 2>&1"
check "nginx is installed" \
  "docker exec ${CONTAINER_NAME} nginx -v 2>&1 | grep nginx >/dev/null"
check "nginx serves the default server-base page" \
  "docker exec ${CONTAINER_NAME} curl -fsS http://127.0.0.1:80/ | grep -x axern-server-base-ok >/dev/null"

echo ""
echo "--- Tooling Checks ---"
check "iproute2 is available" \
  "docker exec ${CONTAINER_NAME} ip -V >/dev/null 2>&1"
check "ping is available" \
  "docker exec ${CONTAINER_NAME} ping -V >/dev/null 2>&1"
check "procps is available" \
  "docker exec ${CONTAINER_NAME} ps -p 1 >/dev/null 2>&1"

echo ""
echo "--- Security Checks ---"
check "ssh password authentication is disabled" \
  "docker exec ${CONTAINER_NAME} sshd -T | grep -i '^passwordauthentication no$' >/dev/null"
check "ssh keyboard-interactive authentication is disabled" \
  "docker exec ${CONTAINER_NAME} sshd -T | grep -i '^kbdinteractiveauthentication no$' >/dev/null"
check "root ssh login is disabled" \
  "docker exec ${CONTAINER_NAME} sshd -T | grep -i '^permitrootlogin no$' >/dev/null"

echo ""
echo "=== Results: ${passed} passed, ${failed} failed ==="

if [ "${failed}" -gt 0 ]; then
  exit 1
fi

echo "Axern server-base runtime smoke checks passed."
