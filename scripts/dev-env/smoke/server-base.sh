run_local_server_base_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local gateway_target="$4"
  local namespace="${prefix}-${env_name}-server-base-smoke-$(date +%s)"
  local env_json run_json run_get run_id environment_id allocation_id
  local go_bin
  go_bin="$(axern_go_bin)"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  cleanup_server_base_run() {
    local rc=$?
    if [ -n "${run_id:-}" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json >/dev/null 2>&1 || true
      local_smoke_wait_for_run_terminal "${run_id}" >/dev/null 2>&1 || true
    fi
    if [ -n "${environment_id:-}" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null 2>&1 || true
    fi
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null 2>&1 || true
    return "${rc}"
  }
  trap cleanup_server_base_run RETURN

  env_json="$(local_smoke_json_once_or_recover_by_namespace environment environments environment "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" environment create -o json --namespace "${namespace}" --template-id server-base)"
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"
  run_json="$(local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" run --detach -o json --namespace "${namespace}" --environment "${environment_id}")"
  run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${run_json}")"
  run_get="$(local_smoke_wait_for_run_status "${run_id}" running)"
  allocation_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["allocation_id"])' <<<"${run_get}")"
  [ -n "${allocation_id}" ]

  local terminal_url terminal_script terminal_payload
  terminal_url="ws://${gateway_target}/terminal/allocation/${allocation_id}"
  terminal_payload="$(base64 <"${AXERN_ROOT}/scripts/dev-env/smoke/server-base-terminal.sh" | tr -d '\n')"
  terminal_script="$(printf 'set -eu\nmkdir -p /tmp/axern-smoke\nprintf %%s %s | base64 -d >/tmp/axern-smoke/server-base-terminal.sh\nbash /tmp/axern-smoke/server-base-terminal.sh\nexit\n' "${terminal_payload}")"
  (cd "${AXERN_ROOT}/gateway/gatewayd" && "${go_bin}" run ./cmd/gateway-terminal-smoke \
    -url "${terminal_url}" -token axern-local-dev -stdin "${terminal_script}" \
    -expect server-base-default-entrypoint-ok -expect-crlf $'server-base-default-entrypoint-ok\r\n')

  local ssh_output_file
  ssh_output_file="$(mktemp)"
  if ! printf '%s\n' "tty" "whoami" 'printf "HOME=%s\n" "$HOME"' \
    'printf "SHELL=%s\n" "$SHELL"' 'printf "PWD=%s\n" "$PWD"' "stty -a" \
    "ls /" "echo server-base-ssh-ok" "exit" |
    "${AXERN_SMOKE_CMD[@]}" ssh --user axern --ssh-option LogLevel=ERROR --shell /bin/bash "${allocation_id}" >"${ssh_output_file}" 2>&1; then
    cat "${ssh_output_file}" >&2
    rm -f "${ssh_output_file}"
    return 1
  fi
  python3 - "${ssh_output_file}" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
required = [b"/dev/pts/", b"axern", b"HOME=/home/axern", b"SHELL=/bin/bash", b"PWD=/home/axern", b"opost", b"onlcr", b"server-base-ssh-ok", b"bin", b"usr"]
missing = [item.decode() for item in required if item not in data]
if missing:
    raise SystemExit(f"server-base SSH output missing {missing}: {data!r}")
if b"bin\n" in data or b"usr\n" in data:
    raise SystemExit(f"server-base SSH output lost CRLF terminal line discipline: {data!r}")
PY
  rm -f "${ssh_output_file}"

  local sudo_output_file
  sudo_output_file="$(mktemp)"
  if ! printf '%s\n' "exec sudo -n ls /root >/dev/null" |
    "${AXERN_SMOKE_CMD[@]}" ssh --user axern --ssh-option LogLevel=ERROR --shell /bin/bash "${allocation_id}" >"${sudo_output_file}" 2>&1; then
    cat "${sudo_output_file}" >&2
    rm -f "${sudo_output_file}"
    return 1
  fi
  rm -f "${sudo_output_file}"

  cleanup_server_base_run
  run_id=""
  environment_id=""
  echo "${prefix}_server_base_smoke_ok=true"
}
