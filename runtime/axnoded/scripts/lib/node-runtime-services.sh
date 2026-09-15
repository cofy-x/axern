#!/usr/bin/env bash


ensure_node_runtime_base_spec() {
  local runtime_bin="$1"
  local output_path="$2"
  if [ ! -x "${runtime_bin}" ]; then
    echo "runtime binary is not executable: ${runtime_bin}" >&2
    return 1
  fi

  mkdir -p "$(dirname "${output_path}")"
  local work_dir output_tmp
  work_dir="$(mktemp -d)"
  output_tmp="$(mktemp "${output_path}.tmp.XXXXXX")"
  if ! (
    cd "${work_dir}"
    "${runtime_bin}" spec >/dev/null
    jq '.process.terminal = false' config.json >"${output_tmp}"
  ); then
    rm -rf "${work_dir}"
    rm -f "${output_tmp}"
    return 1
  fi
  chmod 0644 "${output_tmp}"
  mv "${output_tmp}" "${output_path}"
  rm -rf "${work_dir}"
}
