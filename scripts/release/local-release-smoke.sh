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
output="$("${cli}" "${common[@]}" run python:3.12-slim -- python -c 'print("hello from axern")')"
[[ "${output}" == *"hello from axern"* ]]
"${cli}" "${common[@]}" local down
"${cli}" "${common[@]}" local up --use
"${cli}" "${common[@]}" local reset --force
