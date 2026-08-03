#!/usr/bin/env bash
set -euo pipefail

cli="${AXERN_CLI_BINARY:?AXERN_CLI_BINARY is required}"
smoke_root="$(mktemp -d)"
cleanup() {
  AXERN_HOME="${smoke_root}" "${cli}" local down >/dev/null 2>&1 || true
  AXERN_HOME="${smoke_root}" "${cli}" local reset --force >/dev/null 2>&1 || true
}
trap cleanup EXIT

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
