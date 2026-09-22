#!/usr/bin/env bash


ensure_node_runtime_base_spec() {
  local runtime_bin="$1"
  local output_path="$2"
  if [ ! -x "${runtime_bin}" ]; then
    echo "runtime binary is not executable: ${runtime_bin}" >&2
    return 1
  fi

  mkdir -p "$(dirname "${output_path}")"
  "${AXNODED_BIN:-/usr/local/bin/axnoded}" base-spec "${output_path}"
}
