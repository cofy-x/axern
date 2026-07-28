run_local_server_base_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local gateway_target="$4"
  local namespace="${prefix}-${env_name}-server-base-smoke-$(date +%s)"
  local catalog_json env_json service_json service_get replicas_json terminal_allocation_id
  local service_id=""
  local go_bin
  go_bin="$(axern_go_bin)"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"

  cleanup_server_base_service() {
    local rc=$?
    [ -n "${service_id}" ] || return "${rc}"
    local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
    service_id=""
    return "${rc}"
  }
  trap cleanup_server_base_service RETURN

  catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog list -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); server=next(item for item in data["runtime_templates"] if item["id"] == "server-base"); assert server["image_default_argv"][0].endswith("supervisord")' <<<"${catalog_json}" >/dev/null

  env_json="$(local_smoke_json_once_or_recover_by_namespace environment environments environment "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" environment create -o json --namespace "${namespace}" --template-id server-base)"
  local environment_id
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --readiness-http-port 80 --readiness-http-path / --readiness-period 1s --readiness-timeout 1s)"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"
  [ -n "${service_id}" ]

  local deadline=$((SECONDS + 180))
  service_get=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  if ! python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1' <<<"${service_get}" >/dev/null; then
    echo "server-base service did not become ready: ${service_id}" >&2
    printf '%s\n' "${service_get}" >&2
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${service_id}" >&2 || true
    return 1
  fi

  replicas_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${service_id}")"
  terminal_allocation_id="$(python3 -c '
import json, sys
payload = json.load(sys.stdin)
for replica in payload.get("replicas", []):
    if replica.get("ready") and not replica.get("ended") and not replica.get("outdated"):
        print(replica["id"])
        raise SystemExit(0)
raise SystemExit(1)
' <<<"${replicas_json}")"
  [ -n "${terminal_allocation_id}" ]

  local gateway_url terminal_url terminal_script terminal_payload
  gateway_url="http://${gateway_target}"
  local nginx_body
  nginx_body="$(curl --connect-timeout 2 --max-time 30 -fsS "${gateway_url}/svc/${namespace}/${service_id}/80/")"
  if [ "${nginx_body}" != "axern-server-base-ok" ]; then
    echo "unexpected server-base nginx response: ${nginx_body}" >&2
    return 1
  fi

  terminal_url="ws://${gateway_target}/terminal/allocation/${terminal_allocation_id}"
  terminal_payload="$(base64 <"${AXERN_ROOT}/scripts/dev-env/smoke/server-base-terminal.sh" | tr -d '\n')"
  terminal_script="$(printf 'set -eu\nmkdir -p /tmp/axern-smoke\nprintf %%s %s | base64 -d >/tmp/axern-smoke/server-base-terminal.sh\nbash /tmp/axern-smoke/server-base-terminal.sh\nexit\n' "${terminal_payload}")"
  (cd "${AXERN_ROOT}/gateway/gatewayd" && "${go_bin}" run ./cmd/gateway-terminal-smoke \
    -url "${terminal_url}" \
    -token axern-local-dev \
    -stdin "${terminal_script}" \
    -expect server-base-default-entrypoint-ok \
    -expect-crlf $'server-base-default-entrypoint-ok\r\n')

  local ssh_output_file
  ssh_output_file="$(mktemp)"
  if ! {
    printf '%s\n' \
      "tty" \
      "whoami" \
      'printf "HOME=%s\n" "$HOME"' \
      'printf "SHELL=%s\n" "$SHELL"' \
      'printf "PWD=%s\n" "$PWD"' \
      "stty -a" \
      "ls /" \
      "echo server-base-ssh-ok" \
      "exit"
  } |
    "${AXERN_SMOKE_CMD[@]}" ssh --allocation-id "${terminal_allocation_id}" --user axern --ssh-option LogLevel=ERROR --shell /bin/bash "${service_id}" >"${ssh_output_file}" 2>&1; then
    echo "unexpected server-base axern ssh output:" >&2
    cat "${ssh_output_file}" >&2
    rm -f "${ssh_output_file}"
    return 1
  fi
  if ! python3 - "${ssh_output_file}" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
required = [
    b"/dev/pts/",
    b"axern",
    b"HOME=/home/axern",
    b"SHELL=/bin/bash",
    b"PWD=/home/axern",
    b"opost",
    b"onlcr",
    b"server-base-ssh-ok",
    b"bin",
    b"usr",
]
missing = [item.decode() for item in required if item not in data]
if missing:
    raise SystemExit(f"server-base axern ssh output missing {missing}: {data!r}")
if b"bin\n" in data or b"usr\n" in data:
    raise SystemExit(f"server-base axern ssh output lost CRLF terminal line discipline: {data!r}")
PY
  then
    rm -f "${ssh_output_file}"
    return 1
  fi
  rm -f "${ssh_output_file}"

  local sudo_output_file
  sudo_output_file="$(mktemp)"
  if ! printf '%s\n' "exec sudo -n ls /root >/dev/null" |
    "${AXERN_SMOKE_CMD[@]}" ssh --allocation-id "${terminal_allocation_id}" --user axern --ssh-option LogLevel=ERROR --shell /bin/bash "${service_id}" >"${sudo_output_file}" 2>&1; then
    echo "unexpected server-base sudo ssh output:" >&2
    cat "${sudo_output_file}" >&2
    rm -f "${sudo_output_file}"
    return 1
  fi
  rm -f "${sudo_output_file}"

  cleanup_server_base_service
  echo "${prefix}_server_base_smoke_ok=true"
}
