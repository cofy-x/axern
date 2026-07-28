#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd curl
require_cmd docker
require_cmd python3
require_cmd ssh
go_bin="$(axern_go_bin)"
require_cmd "${go_bin}"

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

namespace="gateway-compose-smoke-$(date +%s)"
service_id=""
baseline_allocation_ids="$(docker exec axern-local-node-1 axctl sandbox list 2>/dev/null | awk 'NR > 1 && $1 ~ /^alloc-/ { print $1 }' || true)"

delete_service() {
  local current="$1"
  [ -n "${current}" ] || return 0
  local_smoke_delete_service "${current}" >/dev/null 2>&1 || true
}

cleanup() {
  delete_service "${service_id}"
}

create_gateway_smoke_service() {
  local service_namespace="$1"
  local created
  if ! created="$(local_smoke_json_once_or_recover_by_namespace service services service "${service_namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${service_namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --argv python --argv -u --argv -c --argv "${service_script}" \
    --readiness-http-port 8080 --readiness-http-path / --readiness-period 1s --readiness-timeout 1s)"; then
    return 1
  fi
  python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${created}"
}

wait_for_ready_allocation() {
  local current_service_id="$1"
  local deadline service_get replicas_json
  deadline=$((SECONDS + 120))
  service_get=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${current_service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  if ! python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1' <<<"${service_get}" >/dev/null; then
    echo "service did not become ready: ${current_service_id}" >&2
    printf '%s\n' "${service_get}" >&2
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${current_service_id}" >&2 || true
    return 1
  fi
  replicas_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${current_service_id}")"
  python3 -c '
import json, sys
payload = json.load(sys.stdin)
for replica in payload.get("replicas", []):
    if replica.get("ready") and not replica.get("ended") and not replica.get("outdated"):
        print(replica["id"])
        raise SystemExit(0)
raise SystemExit(1)
' <<<"${replicas_json}"
}

trap 'cleanup; end_env_lock compose' EXIT

wait_for_compose_allocations_zero() {
  local timeout="${1:-120}"
  local deadline=$((SECONDS + timeout))
  local status_body
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    status_body="$(bash "${AXERN_ROOT}/scripts/dev-env/compose-status.sh" 2>/dev/null || true)"
    if printf '%s\n' "${status_body}" | grep -q '^running_allocation_ids=0$'; then
      echo "running_allocation_ids=0"
      return 0
    fi
    sleep 2
  done
  return 1
}

compose_running_allocation_ids() {
  docker exec axern-local-node-1 axctl sandbox list 2>/dev/null | awk 'NR > 1 && $1 ~ /^alloc-/ { print $1 }'
}

cleanup_new_compose_allocations() {
  local current allocation
  current="$(compose_running_allocation_ids || true)"
  for allocation in ${current}; do
    if is_baseline_allocation "${allocation}"; then
      continue
    fi
    docker exec axern-local-node-1 axctl sandbox delete "${allocation}" >/dev/null 2>&1 || true
  done
}

is_baseline_allocation() {
  local candidate="$1"
  local allocation
  for allocation in ${baseline_allocation_ids}; do
    if [ "${allocation}" = "${candidate}" ]; then
      return 0
    fi
  done
  return 1
}

wait_for_no_new_compose_allocations() {
  local timeout="${1:-30}"
  local deadline=$((SECONDS + timeout))
  local current allocation found_new
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    current="$(compose_running_allocation_ids || true)"
    found_new=0
    for allocation in ${current}; do
      if ! is_baseline_allocation "${allocation}"; then
        found_new=1
        break
      fi
    done
    if [ "${found_new}" -eq 0 ]; then
      if [ -z "${baseline_allocation_ids}" ]; then
        echo "running_allocation_ids=0"
      else
        echo "running_allocation_ids_baseline_preserved=true"
      fi
      return 0
    fi
    sleep 2
  done
  return 1
}

catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog list -o json)"
local_smoke_assert_default_runtime_templates "${catalog_json}"
bundle_catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog bundle list -o json)"
local_smoke_assert_default_agent_bundles "${bundle_catalog_json}"

env_json="$(local_smoke_create_environment "${namespace}")"
environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

service_script='from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"gateway-smoke-ok\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("X-Axern-Gateway-Smoke", "ok")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, fmt, *args):
        pass
ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()'

service_id="$(create_gateway_smoke_service "${namespace}")"
terminal_allocation_id="$(wait_for_ready_allocation "${service_id}")"

gateway_url="http://127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}"
body="$(curl --connect-timeout 2 --max-time 30 -fsS "${gateway_url}/svc/${namespace}/${service_id}/8080/smoke")"
if [ "${body}" != "gateway-smoke-ok" ]; then
  echo "unexpected gateway service response: ${body}" >&2
  exit 1
fi
echo "gateway_service_smoke_ok=true"

terminal_url="ws://127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}/terminal/allocation/${terminal_allocation_id}"
(cd "${AXERN_ROOT}/gateway/gatewayd" && "${go_bin}" run ./cmd/gateway-terminal-smoke \
  -url "${terminal_url}" \
  -token axern-local-dev \
  -stdin $'printf gateway-terminal-fast-ok\n/bin/true\necho gateway-terminal-ok\nexit\n' \
  -expect gateway-terminal-ok \
  -expect-crlf $'gateway-terminal-fast-ok\r\n')

ssh_key="${COMPOSE_STATE_DIR}/ssh/gateway_client_ed25519"
if [ ! -s "${ssh_key}" ]; then
  echo "missing compose gateway SSH client key: ${ssh_key}; run make local-compose-up" >&2
  exit 1
fi
ssh_output_file="$(mktemp)"
if ! printf 'printf gateway-ssh-fast-ok\n/bin/true\ntty\nstty -a\nls /\necho gateway-ssh-ok\nexit\n' | ssh -tt \
  -i "${ssh_key}" \
  -p "${COMPOSE_GATEWAY_SSH_PORT}" \
  -o BatchMode=yes \
  -o ConnectTimeout=10 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  -o GlobalKnownHostsFile=/dev/null \
  -o LogLevel=ERROR \
  "${terminal_allocation_id}@127.0.0.1" >"${ssh_output_file}" 2>&1; then
  echo "unexpected gateway ssh terminal output:" >&2
  cat "${ssh_output_file}" >&2
  rm -f "${ssh_output_file}"
  exit 1
fi
if ! python3 - "${ssh_output_file}" <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
required = [b"/dev/pts/", b"opost", b"onlcr", b"gateway-ssh-fast-ok", b"gateway-ssh-ok", b"bin", b"usr"]
missing = [item.decode() for item in required if item not in data]
if missing:
    raise SystemExit(f"gateway ssh output missing {missing}: {data!r}")
if b"bin\n" in data or b"usr\n" in data:
    raise SystemExit(f"gateway ssh output lost CRLF terminal line discipline: {data!r}")
if b"gateway-ssh-fast-ok\n" in data:
    raise SystemExit(f"gateway ssh fast command lost CRLF terminal line discipline: {data!r}")
PY
then
  rm -f "${ssh_output_file}"
  exit 1
fi
rm -f "${ssh_output_file}"
echo "gateway_ssh_smoke_ok=true"
echo "gateway_ssh_login=ssh -i ${ssh_key} -p ${COMPOSE_GATEWAY_SSH_PORT} <allocation_id>@127.0.0.1"

delete_service "${service_id}"
service_id=""

if [ -z "${baseline_allocation_ids}" ]; then
  wait_for_cleanup="wait_for_compose_allocations_zero"
else
  wait_for_cleanup="wait_for_no_new_compose_allocations"
fi

if "${wait_for_cleanup}" 120; then
  echo "compose_gateway_smoke_ok=true"
  exit 0
fi

echo "compose gateway smoke cleanup did not reach running_allocation_ids=0; trying node-local cleanup for smoke allocations" >&2
cleanup_new_compose_allocations
if wait_for_no_new_compose_allocations 30; then
  echo "gateway_cleanup_fallback=axctl_delete"
  echo "compose_gateway_smoke_ok=true"
  exit 0
fi

echo "compose gateway smoke cleanup did not reach running_allocation_ids=0" >&2
bash "${AXERN_ROOT}/scripts/dev-env/compose-status.sh" >&2 || true
exit 1
