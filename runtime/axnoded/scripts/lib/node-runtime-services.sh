#!/usr/bin/env bash

setup_node_runtime_volume_defaults() {
  VOLUMED_BIN="${VOLUMED_BIN:-/usr/local/bin/volumed}"
  VOLUMED_SOCKET="${VOLUMED_SOCKET:-/run/volumed/volumed.sock}"
  VOLUMED_ROOT="${VOLUMED_ROOT:-/var/lib/volumed}"
  VOLUMED_LOCAL_ROOT="${VOLUMED_LOCAL_ROOT:-${VOLUMED_ROOT}/local}"
  VOLUMED_LOG="${VOLUMED_LOG:-/tmp/volumed.log}"
  NODE_RUNTIME_VOLUMED_PID="${NODE_RUNTIME_VOLUMED_PID:-}"
}

start_node_runtime_volumed() {
  setup_node_runtime_volume_defaults
  mkdir -p "$(dirname "${VOLUMED_SOCKET}")" "${VOLUMED_ROOT}" "${VOLUMED_LOCAL_ROOT}"

  "${VOLUMED_BIN}" \
    -root "${VOLUMED_ROOT}" \
    -socket "${VOLUMED_SOCKET}" \
    -local-root "${VOLUMED_LOCAL_ROOT}" \
    >"${VOLUMED_LOG}" 2>&1 &

  NODE_RUNTIME_VOLUMED_PID=$!

  for _ in $(seq 1 30); do
    if [ -S "${VOLUMED_SOCKET}" ]; then
      return 0
    fi
    sleep 1
  done

  echo "volumed did not become ready in time" >&2
  tail -n 120 "${VOLUMED_LOG}" >&2 || true
  return 1
}

stop_node_runtime_volumed() {
  if [ -n "${NODE_RUNTIME_VOLUMED_PID:-}" ] && kill -0 "${NODE_RUNTIME_VOLUMED_PID}" >/dev/null 2>&1; then
    kill "${NODE_RUNTIME_VOLUMED_PID}" >/dev/null 2>&1 || true
    wait "${NODE_RUNTIME_VOLUMED_PID}" >/dev/null 2>&1 || true
  fi
}

tail_node_runtime_volumed_log() {
  local lines="${1:-120}"
  setup_node_runtime_volume_defaults
  tail -n "${lines}" "${VOLUMED_LOG}" >&2 || true
}
