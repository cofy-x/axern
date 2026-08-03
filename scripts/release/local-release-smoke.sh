#!/usr/bin/env bash
set -euo pipefail

cli="${AXERN_CLI_BINARY:?AXERN_CLI_BINARY is required}"
smoke_root="$(mktemp -d)"
cleanup() {
  AXERN_HOME="${smoke_root}" "${cli}" local down >/dev/null 2>&1 || true
  AXERN_HOME="${smoke_root}" "${cli}" local reset --force >/dev/null 2>&1 || true
}

diagnostics() {
  echo "local release smoke failed; collecting diagnostics" >&2
  for output in run.stdout run.stderr exit.stdout exit.stderr; do
    if [[ -s "${smoke_root}/${output}" ]]; then
      echo "--- ${output} ---" >&2
      sed -n '1,240p' "${smoke_root}/${output}" >&2
    fi
  done
  echo "--- local status ---" >&2
  AXERN_HOME="${smoke_root}" "${cli}" --config "${smoke_root}/config.json" --timeout 30s local status >&2 || true
  echo "--- local logs ---" >&2
  AXERN_HOME="${smoke_root}" "${cli}" --config "${smoke_root}/config.json" --timeout 30s local logs --tail 200 >&2 || true
}

on_exit() {
  status=$?
  if [[ "${status}" -ne 0 ]]; then
    diagnostics
  fi
  cleanup
  return "${status}"
}
trap on_exit EXIT

export AXERN_HOME="${smoke_root}"
config="${smoke_root}/config.json"
common=(--config "${config}" --timeout 10m)

"${cli}" "${common[@]}" local up --use
"${cli}" "${common[@]}" local status --output json | grep -q '"state": "running"'
stdout_file="${smoke_root}/run.stdout"
stderr_file="${smoke_root}/run.stderr"
"${cli}" "${common[@]}" run python:3.12-slim -- \
  python -c 'import sys; print("hello from axern"); print("hello from stderr", file=sys.stderr)' \
  >"${stdout_file}" 2>"${stderr_file}"
grep -Fxq "hello from axern" "${stdout_file}"
grep -Fq "hello from stderr" "${stderr_file}"

set +e
"${cli}" "${common[@]}" run python:3.12-slim -- python -c 'raise SystemExit(7)' \
  >"${smoke_root}/exit.stdout" 2>"${smoke_root}/exit.stderr"
run_status=$?
set -e
if [[ "${run_status}" -ne 7 ]]; then
  echo "foreground Run returned ${run_status}, want workload exit code 7" >&2
  exit 1
fi
"${cli}" "${common[@]}" local down
"${cli}" "${common[@]}" local up --use
"${cli}" "${common[@]}" local reset --force
