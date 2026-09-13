#!/usr/bin/env bash
set -euo pipefail

SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
VERIFY_SMOKE_BIN="${VERIFY_SMOKE_BIN:-/usr/local/bin/verify-smoke}"

"${VERIFY_SMOKE_BIN}" \
  -address "${SOCKET_ADDRESS}" \
  -rootfs /opt/sample-rootfs \
  -stdout /tmp/axnoded-verify.stdout \
  -stderr /tmp/axnoded-verify.stderr
