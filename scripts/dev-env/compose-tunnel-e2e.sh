#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'cleanup; end_env_lock compose' EXIT

require_cmd curl
require_cmd docker
require_cmd jq
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
work_dir="$(mktemp -d)"
upstream_pid=""
agent_upstream_pid=""
service_ids=()
tunnel_pids=()
runtime_results=()
marker="axern-tunnel-e2e-ok-$(date +%s)"
agent_marker="axern-agent-e2e-ok-$(date +%s)"
agent_upstream_token="axern-compose-agent-upstream-token-$(date +%s)"
runtime_list="${AXERN_TUNNEL_E2E_RUNTIMES:-runsc runc}"
restart_relay="${AXERN_TUNNEL_E2E_RESTART_RELAY:-true}"
relay_restart_done=false
verify_renew="${AXERN_TUNNEL_E2E_VERIFY_RENEW:-true}"
renew_check_done=false
verify_revoke="${AXERN_TUNNEL_E2E_VERIFY_REVOKE:-true}"
revoke_check_done=false
verify_expire="${AXERN_TUNNEL_E2E_VERIFY_EXPIRE:-true}"
expire_check_done=false
verify_node_restart="${AXERN_TUNNEL_E2E_VERIFY_NODE_TUNNELD_RESTART:-true}"
node_restart_check_done=false
verify_agent="${AXERN_TUNNEL_E2E_VERIFY_AGENT:-true}"
agent_check_done=false
node_paused=false

cleanup() {
  local pid service_id
  if [ "${node_paused}" = "true" ]; then
    docker unpause "${node_container}" >/dev/null 2>&1 || true
    node_paused=false
  fi
  for pid in "${tunnel_pids[@]:-}"; do
    if [ -n "${pid}" ] && kill -0 "${pid}" >/dev/null 2>&1; then
      kill -CONT "${pid}" >/dev/null 2>&1 || true
      kill "${pid}" >/dev/null 2>&1 || true
      wait "${pid}" >/dev/null 2>&1 || true
    fi
  done
  for service_id in "${service_ids[@]:-}"; do
    [ -n "${service_id}" ] || continue
    local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
  done
  if [ -n "${upstream_pid}" ] && kill -0 "${upstream_pid}" >/dev/null 2>&1; then
    kill "${upstream_pid}" >/dev/null 2>&1 || true
    wait "${upstream_pid}" >/dev/null 2>&1 || true
  fi
  if [ -n "${agent_upstream_pid}" ] && kill -0 "${agent_upstream_pid}" >/dev/null 2>&1; then
    kill "${agent_upstream_pid}" >/dev/null 2>&1 || true
    wait "${agent_upstream_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}"
}

json_field() {
  local expr="$1"
  python3 -c "import json,sys; data=json.load(sys.stdin); print(${expr})"
}

assert_tunnel_event() {
  local session_id="$1"
  local event_type="$2"
  local events_json
  events_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel events -o json --limit 20 "${session_id}")"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
want = sys.argv[1]
for event in payload.get("events", []):
    if event.get("event_type") == want:
        raise SystemExit(0)
raise SystemExit(f"missing tunnel event {want}")
' "${event_type}" <<<"${events_json}"
}

pick_local_port() {
  python3 - <<'PY'
import socket

sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
}

start_upstream() {
  local port="$1"
  printf '%s\n' "${marker}" >"${work_dir}/index.txt"
  python3 -m http.server "${port}" --bind 127.0.0.1 --directory "${work_dir}" >/dev/null 2>&1 &
  upstream_pid="$!"

  local deadline=$((SECONDS + 10))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl --connect-timeout 1 --max-time 2 -fsS "http://127.0.0.1:${port}/index.txt" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "local upstream did not become ready on 127.0.0.1:${port}" >&2
  return 1
}

start_agent_anthropic_upstream() {
  local port="$1"
  AXERN_AGENT_UPSTREAM_TOKEN="${agent_upstream_token}" AXERN_AGENT_MARKER="${agent_marker}" python3 - "${port}" <<'PY' >/dev/null 2>&1 &
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

token = os.environ["AXERN_AGENT_UPSTREAM_TOKEN"]
marker = os.environ["AXERN_AGENT_MARKER"]

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("content-length") or "0")
        body = self.rfile.read(length).decode()
        if self.path != "/anthropic/v1/messages?probe=1":
            self.send_error(404, f"unexpected path {self.path}")
            return
        if self.headers.get("x-api-key") != token:
            self.send_error(401, "missing upstream x-api-key token")
            return
        if "compose-adapter-probe" not in body:
            self.send_error(400, "missing probe body")
            return
        payload = json.dumps({"id": "agent-smoke", "content": marker}).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_):
        return

ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
  agent_upstream_pid="$!"

  local deadline=$((SECONDS + 10))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl --connect-timeout 1 --max-time 2 -fsS -X POST \
      -H "x-api-key: ${agent_upstream_token}" \
      -d '{"probe":"compose-adapter-probe"}' \
      "http://127.0.0.1:${port}/anthropic/v1/messages?probe=1" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "fake agent Anthropic upstream did not become ready on 127.0.0.1:${port}" >&2
  return 1
}

compose_runtime_binary() {
  local runtime_class="$1"
  docker exec "${node_container}" sh -lc "command -v ${runtime_class}" | tr -d '\r'
}

wait_for_ready_allocation() {
  local service_id="$1"
  local service_get replicas_json
  local deadline=$((SECONDS + 180))
  service_get=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      replicas_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${service_id}")"
      if python3 -c '
import json
import sys

payload = json.load(sys.stdin)
for replica in payload.get("replicas", []):
    if replica.get("ready") and not replica.get("ended") and not replica.get("outdated"):
        print(replica["id"])
        raise SystemExit(0)
raise SystemExit(1)
' <<<"${replicas_json}"; then
        return 0
      fi
    fi
    sleep 2
  done

  echo "service did not become ready: ${service_id}" >&2
  printf '%s\n' "${service_get}" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${service_id}" >&2 || true
  return 1
}

dump_tunnel_diagnostics() {
  local runtime_class="$1"
  local service_id="$2"
  local tunnel_log="$3"
  echo "compose tunnel e2e diagnostics runtime=${runtime_class} service=${service_id}" >&2
  echo "--- tunnel open log ---" >&2
  sed -n '1,220p' "${tunnel_log}" >&2 || true
  echo "--- service get ---" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" >&2 || true
  echo "--- service replicas ---" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${service_id}" >&2 || true
  echo "--- node-tunneld log ---" >&2
  docker exec "${node_container}" sh -lc 'tail -n 160 /var/log/axnoded/node-tunneld.log 2>/dev/null || true' >&2 || true
  echo "--- axnoded log ---" >&2
  docker exec "${node_container}" sh -lc 'tail -n 120 /var/log/axnoded/axnoded.log 2>/dev/null || true' >&2 || true
  echo "--- tunneld logs ---" >&2
  docker logs --tail 160 "${COMPOSE_PROJECT_NAME}-tunneld-1" >&2 || true
}

exec_in_allocation() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  if [ -z "${binary}" ]; then
    echo "runtime binary not found in ${node_container}: ${runtime_class}" >&2
    return 1
  fi

  local root_dir="/var/lib/axnoded/root/${runtime_class}"
  local python_code
  python_code="$(cat <<PY
import sys
import urllib.request

url = "http://127.0.0.1:${remote_port}/index.txt"
try:
    with urllib.request.urlopen(url, timeout=3) as response:
        sys.stdout.write(response.read().decode().strip())
except Exception as exc:
    sys.stderr.write(str(exc) + "\\n")
    raise SystemExit(1)
PY
)"
  docker exec "${node_container}" "${binary}" --root "${root_dir}" exec "${allocation_id}" python -c "${python_code}"
}

exec_in_allocation_post() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local request_path="$4"
  local request_body="$5"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  if [ -z "${binary}" ]; then
    echo "runtime binary not found in ${node_container}: ${runtime_class}" >&2
    return 1
  fi

  local root_dir="/var/lib/axnoded/root/${runtime_class}"
  local python_code
  python_code="$(cat <<PY
import sys
import urllib.request

url = "http://127.0.0.1:${remote_port}${request_path}"
body = '''${request_body}'''.encode()
request = urllib.request.Request(url, data=body, method="POST", headers={
    "authorization": "Bearer remote-ignored-token",
    "x-api-key": "remote-ignored-key",
    "content-type": "application/json",
})
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        sys.stdout.write(response.read().decode().strip())
except Exception as exc:
    sys.stderr.write(str(exc) + "\\n")
    raise SystemExit(1)
PY
)"
  docker exec "${node_container}" "${binary}" --root "${root_dir}" exec "${allocation_id}" python3 -c "${python_code}"
}

wait_tunnel_remote_port() {
  local tunnel_log="$1"
  local timeout_seconds="${2:-20}"
  local tunnel_pid="${3:-}"
  local deadline=$((SECONDS + timeout_seconds))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local port
    port="$(python3 - "${tunnel_log}" <<'PY' || true
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(r"(?:Remote bind: 127\.0\.0\.1:|Remote agent base URL: http://127\.0\.0\.1:)(\d+)", text)
if match:
    print(match.group(1))
PY
)"
    if [ -n "${port}" ]; then
      printf '%s\n' "${port}"
      return 0
    fi
    if [ -n "${tunnel_pid}" ] && ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "tunnel process exited before printing an allocated remote port" >&2
      sed -n '1,80p' "${tunnel_log}" >&2 || true
      return 1
    fi
    sleep 0.2
  done
  echo "tunnel open did not print an allocated remote port" >&2
  sed -n '1,80p' "${tunnel_log}" >&2 || true
  return 1
}

wait_tunnel_session_id() {
  local tunnel_log="$1"
  local timeout_seconds="${2:-20}"
  local tunnel_pid="${3:-}"
  local deadline=$((SECONDS + timeout_seconds))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local session_id
    session_id="$(python3 - "${tunnel_log}" <<'PY' || true
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(r"Tunnel session(?::| )\s*(\S+)", text)
if match:
    print(match.group(1))
PY
)"
    if [ -n "${session_id}" ]; then
      printf '%s\n' "${session_id}"
      return 0
    fi
    if [ -n "${tunnel_pid}" ] && ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "tunnel process exited before printing a session id" >&2
      sed -n '1,80p' "${tunnel_log}" >&2 || true
      return 1
    fi
    sleep 0.2
  done
  echo "tunnel open did not print a session id" >&2
  sed -n '1,80p' "${tunnel_log}" >&2 || true
  return 1
}

wait_tunnel_response() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local tunnel_pid="$4"
  local deadline="$5"
  local observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "tunnel process exited early for runtime=${runtime_class}" >&2
      return 2
    fi
    observed="$(exec_in_allocation "${runtime_class}" "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" = "${marker}" ]; then
      return 0
    fi
    sleep 2
  done
  echo "unexpected tunnel response runtime=${runtime_class}: ${observed}" >&2
  return 1
}

exec_agent_claude_settings_post() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  if [ -z "${binary}" ]; then
    echo "runtime binary not found in ${node_container}: ${runtime_class}" >&2
    return 1
  fi

  local root_dir="/var/lib/axnoded/root/${runtime_class}"
  local python_code
  python_code="$(cat <<PY
import json
import pathlib
import sys
import urllib.request

settings_path = pathlib.Path("/home/axern/.claude/settings.json")
settings = json.loads(settings_path.read_text())
env = settings.get("env") or {}
token = env.get("ANTHROPIC_API_KEY") or ""
url = "http://127.0.0.1:${remote_port}/v1/messages?probe=1"
body = b'{"probe":"compose-adapter-probe"}'
request = urllib.request.Request(url, data=body, method="POST", headers={
    "authorization": "Bearer " + token,
    "x-api-key": token,
    "content-type": "application/json",
})
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        sys.stdout.write(response.read().decode().strip())
except Exception as exc:
    sys.stderr.write(str(exc) + "\\n")
    raise SystemExit(1)
PY
)"
  docker exec "${node_container}" "${binary}" --root "${root_dir}" exec "${allocation_id}" python3 -c "${python_code}"
}

wait_agent_profile_response() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local tunnel_pid="$4"
  local deadline="$5"
  local observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "agent tunnel process exited early for runtime=${runtime_class}" >&2
      return 2
    fi
    observed="$(exec_agent_claude_settings_post "${runtime_class}" "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if python3 - "${observed}" "${agent_marker}" <<'PY' >/dev/null 2>&1; then
import json
import sys

try:
    payload = json.loads(sys.argv[1])
except json.JSONDecodeError:
    raise SystemExit(1)
raise SystemExit(0 if payload.get("content") == sys.argv[2] else 1)
PY
      return 0
    fi
    sleep 2
  done
  echo "unexpected agent tunnel response runtime=${runtime_class}: ${observed}" >&2
  return 1
}

write_agent_workspace_marker() {
  local runtime_class="$1"
  local allocation_id="$2"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  docker exec "${node_container}" "${binary}" --root "/var/lib/axnoded/root/${runtime_class}" \
    exec "${allocation_id}" sh -lc "printf '%s\\n' '${agent_marker}' > /home/axern/workspace/compose-e2e-marker"
}

read_agent_workspace_marker() {
  local runtime_class="$1"
  local allocation_id="$2"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  docker exec "${node_container}" "${binary}" --root "/var/lib/axnoded/root/${runtime_class}" \
    exec "${allocation_id}" sh -lc 'cat /home/axern/workspace/compose-e2e-marker'
}

assert_agent_workspace_marker_absent() {
  local runtime_class="$1"
  local allocation_id="$2"
  local binary
  binary="$(compose_runtime_binary "${runtime_class}")"
  docker exec "${node_container}" "${binary}" --root "/var/lib/axnoded/root/${runtime_class}" \
    exec "${allocation_id}" sh -lc 'test ! -e /home/axern/workspace/compose-e2e-marker'
}

assert_agent_claim_directory_absent() {
  local claim_id="$1"
  if [ -z "${claim_id}" ]; then
    echo "agent workspace deletion returned no claim id" >&2
    return 1
  fi
  docker exec "${node_container}" sh -lc 'test ! -e "/var/lib/volumed/local/$1"' sh "${claim_id}"
}

wait_agent_workspace_suspended() {
  local config_file="$1"
  local service_id="$2"
  local workspace="$3"
  local profile="$4"
  local service_json replicas_json workspaces_json
  local deadline=$((SECONDS + 90))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_json="$(AXERN_CONFIG="${config_file}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    replicas_json="$(AXERN_CONFIG="${config_file}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${service_id}" 2>/dev/null || true)"
    workspaces_json="$(AXERN_CONFIG="${config_file}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" agent list -o json --workspace "${workspace}" --profile "${profile}" 2>/dev/null || true)"
    if python3 -c 'import json,sys; data=json.load(sys.stdin)["service"]; raise SystemExit(0 if data["id"] == sys.argv[1] and data["replicas"] == 0 and data["ready_replicas"] == 0 else 1)' "${service_id}" <<<"${service_json}" >/dev/null 2>&1 && \
      python3 -c 'import json,sys; replicas=json.load(sys.stdin).get("replicas", []); raise SystemExit(0 if all(item.get("ended") for item in replicas) else 1)' <<<"${replicas_json}" >/dev/null 2>&1 && \
      python3 -c 'import json,sys; items=json.load(sys.stdin); expected=(sys.argv[1],sys.argv[2],sys.argv[3]); actual=[(item["service_id"],item["workspace"],item["profile"]) for item in items if item["lifecycle_state"] == "suspended"]; raise SystemExit(0 if actual == [expected] else 1)' "${service_id}" "${workspace}" "${profile}" <<<"${workspaces_json}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "agent workspace did not suspend: ${workspace} service=${service_id}" >&2
  printf '%s\n' "${service_json}" "${replicas_json}" "${workspaces_json}" >&2
  return 1
}

start_agent_connection() {
  local config_file="$1"
  local profile="$2"
  local workspace="$3"
  local suffix="$4"
  agent_log="${work_dir}/agent-product-${suffix}.log"
  AXERN_CONFIG="${config_file}" \
  "${AXERN_SMOKE_CMD[@]}" agent connect \
    --profile "${profile}" \
    --workspace "${workspace}" \
    --service-timeout 180s \
    --ready-timeout 60s \
    >"${agent_log}" 2>&1 &
  agent_pid="$!"
  tunnel_pids+=("${agent_pid}")
  agent_session_id="$(wait_tunnel_session_id "${agent_log}" 200 "${agent_pid}")"
  agent_remote_port="$(wait_tunnel_remote_port "${agent_log}" 200 "${agent_pid}")"
  agent_service_id="$(python3 - "${agent_log}" <<'PY'
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(r"Agent service (?:created|reused):\s*(\S+)", text)
if match:
    print(match.group(1))
PY
)"
  agent_allocation_id="$(python3 - "${agent_log}" <<'PY'
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(r"Remote runtime ready: allocation=(\S+)", text)
if match:
    print(match.group(1))
PY
)"
  if [ -z "${agent_service_id}" ] || [ -z "${agent_allocation_id}" ]; then
    echo "agent output did not include service and allocation ids" >&2
    sed -n '1,160p' "${agent_log}" >&2 || true
    return 1
  fi
  if grep -q "${agent_upstream_token}" "${agent_log}"; then
    echo "agent output leaked upstream token" >&2
    return 1
  fi
}

close_agent_connection() {
  local config_file="$1"
  AXERN_CONFIG="${config_file}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel revoke -o json --reason e2e-agent-product "${agent_session_id}" >/dev/null
  wait "${agent_pid}" >/dev/null 2>&1 || true
}

wait_tunnel_unavailable() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local deadline="$4"
  local observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    observed="$(exec_in_allocation "${runtime_class}" "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" != "${marker}" ]; then
      return 0
    fi
    sleep 2
  done
  echo "tunnel still reachable runtime=${runtime_class} remote_port=${remote_port}" >&2
  return 1
}

verify_relay_restart_once() {
  local runtime_class="$1"
  local service_id="$2"
  local allocation_id="$3"
  local remote_port="$4"
  local tunnel_pid="$5"
  local tunnel_log="$6"

  if [ "${restart_relay}" != "true" ] || [ "${relay_restart_done}" = "true" ]; then
    return 0
  fi
  relay_restart_done=true
  echo "tunnel_e2e_runtime=${runtime_class} phase=restart_relay"
  docker restart "${COMPOSE_PROJECT_NAME}-tunneld-1" >/dev/null
  if ! wait_tunnel_response "${runtime_class}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" $((SECONDS + 120)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
    return 1
  fi
  echo "tunnel_e2e_runtime=${runtime_class} relay_restart_recovered=true"
}

verify_renew_once() {
  local runtime_class="$1"
  local service_id="$2"
  local allocation_id="$3"
  local remote_port="$4"
  local tunnel_pid="$5"
  local tunnel_log="$6"
  local tunnel_session_id="$7"
  local before_json before_expires after_json after_expires
  if [ "${verify_renew}" != "true" ] || [ "${renew_check_done}" = "true" ]; then
    return 0
  fi
  renew_check_done=true
  echo "tunnel_e2e_runtime=${runtime_class} phase=renew_lease"
  before_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${tunnel_session_id}")"
  before_expires="$(json_field 'data["expires_at"]' <<<"${before_json}")"
  sleep 40
  after_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${tunnel_session_id}")"
  after_expires="$(json_field 'data["expires_at"]' <<<"${after_json}")"
  python3 - "${before_expires}" "${after_expires}" <<'PY'
import datetime
import re
import sys

def parse_rfc3339(value):
    match = re.fullmatch(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})", value)
    if not match:
        raise ValueError(f"invalid RFC3339 timestamp: {value}")
    base, fraction, zone = match.groups()
    fraction = (fraction or "0")[:6].ljust(6, "0")
    if zone == "Z":
        zone = "+00:00"
    return datetime.datetime.fromisoformat(f"{base}.{fraction}{zone}")

before = parse_rfc3339(sys.argv[1])
after = parse_rfc3339(sys.argv[2])
if after <= before:
    raise SystemExit(f"tunnel lease was not renewed: before={before.isoformat()} after={after.isoformat()}")
PY
  if ! wait_tunnel_response "${runtime_class}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" $((SECONDS + 60)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
    return 1
  fi
  assert_tunnel_event "${tunnel_session_id}" "renewed"
  echo "tunnel_e2e_runtime=${runtime_class} lease_renewed=true before=${before_expires} after=${after_expires}"
}

verify_node_tunneld_restart_once() {
  local runtime_class="$1"
  local service_id="$2"
  local allocation_id="$3"
  local remote_port="$4"
  local tunnel_pid="$5"
  local tunnel_log="$6"
  local before_pid after_pid
  if [ "${verify_node_restart}" != "true" ] || [ "${node_restart_check_done}" = "true" ] || [ "${runtime_class}" != "runc" ]; then
    return 0
  fi
  node_restart_check_done=true
  echo "tunnel_e2e_runtime=${runtime_class} phase=node_tunneld_restart"
  before_pid="$(docker exec "${node_container}" sh -lc 'pidof node-tunneld | awk "{print $1}"')"
  if [ -z "${before_pid}" ]; then
    echo "node-tunneld pid not found before restart check" >&2
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
    return 1
  fi
  docker exec "${node_container}" sh -lc "kill -TERM ${before_pid}"
  local deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    after_pid="$(docker exec "${node_container}" sh -lc 'pidof node-tunneld | awk "{print $1}"' 2>/dev/null || true)"
    if [ -n "${after_pid}" ] && [ "${after_pid}" != "${before_pid}" ]; then
      if wait_tunnel_response "${runtime_class}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" $((SECONDS + 90)); then
        echo "tunnel_e2e_runtime=${runtime_class} node_tunneld_restarted=true before=${before_pid} after=${after_pid}"
        return 0
      fi
      break
    fi
    sleep 1
  done
  dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
  return 1
}

verify_agent_product_once() {
	local runtime_class="$1"
	local upstream_port="$2"
	local claude_profile codex_profile workspace namespace agent_config current_config
	local first_service_id first_allocation_id resumed_allocation_id observed list_json delete_json first_claim_id second_service_id second_claim_id
	if [ "${verify_agent}" != "true" ] || [ "${agent_check_done}" = "true" ]; then
		return 0
	fi
	agent_check_done=true
	echo "tunnel_e2e_runtime=${runtime_class} phase=agent_workspace"
	claude_profile="compose-claude-${runtime_class}-$(date +%s)"
	codex_profile="compose-codex-${runtime_class}-$(date +%s)"
	workspace="compose-workspace-${runtime_class}-$(date +%s)"
	namespace="agent-compose-e2e-${runtime_class}-$(date +%s)"
	agent_config="${work_dir}/agent-config-${runtime_class}.json"
	current_config="$(axern_config_file)"
	if [ -s "${current_config}" ]; then
		jq 'del(.agent_profiles)' "${current_config}" >"${agent_config}"
	else
		printf '{}\n' >"${agent_config}"
	fi
	AXERN_CONFIG="${agent_config}" \
	"${AXERN_SMOKE_CMD[@]}" agent profile set "${claude_profile}" \
		--agent claude-code \
		--provider anthropic \
		--upstream "http://127.0.0.1:${upstream_port}/anthropic" \
		--token "${agent_upstream_token}" \
		--namespace "${namespace}" \
		--model compose-sonnet \
		--agent-config api_timeout_ms=3000000 >/dev/null
	AXERN_CONFIG="${agent_config}" \
	"${AXERN_SMOKE_CMD[@]}" agent profile set "${codex_profile}" \
		--agent codex \
		--provider openai \
		--upstream "http://127.0.0.1:${upstream_port}/openai/v1" \
		--token "${agent_upstream_token}" \
		--namespace "${namespace}" \
		--model compose-codex >/dev/null

	start_agent_connection "${agent_config}" "${claude_profile}" "${workspace}" "${runtime_class}-create"
	first_service_id="${agent_service_id}"
	first_allocation_id="${agent_allocation_id}"
	service_ids+=("${first_service_id}")
	if ! wait_agent_profile_response "${runtime_class}" "${first_allocation_id}" "${agent_remote_port}" "${agent_pid}" $((SECONDS + 90)); then
		echo "agent tunnel did not reach fake Anthropic upstream" >&2
		dump_tunnel_diagnostics "${runtime_class}" "${first_service_id}" "${agent_log}"
		return 1
	fi
	write_agent_workspace_marker "${runtime_class}" "${first_allocation_id}"
	close_agent_connection "${agent_config}"
	AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent stop --workspace "${workspace}" >/dev/null
	wait_agent_workspace_suspended "${agent_config}" "${first_service_id}" "${workspace}" "${claude_profile}"

	start_agent_connection "${agent_config}" "${claude_profile}" "${workspace}" "${runtime_class}-resume"
	resumed_allocation_id="${agent_allocation_id}"
	if [ "${agent_service_id}" != "${first_service_id}" ] || [ "${resumed_allocation_id}" = "${first_allocation_id}" ]; then
		echo "agent workspace did not preserve service identity or replace allocation" >&2
		return 1
	fi
	observed="$(read_agent_workspace_marker "${runtime_class}" "${resumed_allocation_id}")"
	if [ "${observed}" != "${agent_marker}" ]; then
		echo "agent workspace marker was not preserved after resume: ${observed}" >&2
		return 1
	fi
	close_agent_connection "${agent_config}"
	AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent stop --workspace "${workspace}" >/dev/null
	wait_agent_workspace_suspended "${agent_config}" "${first_service_id}" "${workspace}" "${claude_profile}"

	start_agent_connection "${agent_config}" "${codex_profile}" "${workspace}" "${runtime_class}-switch"
	if [ "${agent_service_id}" != "${first_service_id}" ] || [ "${agent_allocation_id}" = "${resumed_allocation_id}" ]; then
		echo "agent profile switch did not preserve service identity or replace allocation" >&2
		return 1
	fi
	observed="$(read_agent_workspace_marker "${runtime_class}" "${agent_allocation_id}")"
	if [ "${observed}" != "${agent_marker}" ]; then
		echo "agent workspace marker was not preserved after profile switch: ${observed}" >&2
		return 1
	fi
	list_json="$(AXERN_CONFIG="${agent_config}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" agent list -o json --workspace "${workspace}")"
	python3 -c 'import json,sys; items=json.load(sys.stdin); expected=(sys.argv[1],sys.argv[2],sys.argv[3]); actual=[(item["service_id"],item["profile"],item["lifecycle_state"]) for item in items]; raise SystemExit(0 if actual == [expected] else 1)' "${first_service_id}" "${codex_profile}" running <<<"${list_json}"
	close_agent_connection "${agent_config}"
	AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent stop --workspace "${workspace}" >/dev/null
	wait_agent_workspace_suspended "${agent_config}" "${first_service_id}" "${workspace}" "${codex_profile}"
	docker pause "${node_container}" >/dev/null
	node_paused=true
	if AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent workspace delete --workspace "${workspace}" --yes --timeout 3s >/dev/null 2>&1; then
		echo "agent workspace deletion completed while its storage node was unavailable" >&2
		return 1
	fi
	list_json="$(AXERN_CONFIG="${agent_config}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" agent list -o json --workspace "${workspace}")"
	python3 -c 'import json,sys; items=json.load(sys.stdin); assert len(items) == 1 and items[0]["lifecycle_state"] == "deleting"' <<<"${list_json}"
	docker unpause "${node_container}" >/dev/null
	node_paused=false
	delete_json="$(AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent workspace delete -o json --workspace "${workspace}" --yes --timeout 180s)"
	first_claim_id="$(python3 -c 'import json,sys; data=json.load(sys.stdin); ids=data.get("claim_ids", []); print(ids[0] if ids else "")' <<<"${delete_json}")"
	python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["state"] == "deleted" and data["service_id"] == sys.argv[1] and data.get("completed_at")' "${first_service_id}" <<<"${delete_json}"
	assert_agent_claim_directory_absent "${first_claim_id}"
	list_json="$(AXERN_CONFIG="${agent_config}" local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" agent list -o json --workspace "${workspace}")"
	python3 -c 'import json,sys; assert json.load(sys.stdin) == []' <<<"${list_json}"

	start_agent_connection "${agent_config}" "${codex_profile}" "${workspace}" "${runtime_class}-recreate"
	second_service_id="${agent_service_id}"
	service_ids+=("${second_service_id}")
	if [ "${second_service_id}" = "${first_service_id}" ]; then
		echo "recreated agent workspace reused deleted service identity" >&2
		return 1
	fi
	assert_agent_workspace_marker_absent "${runtime_class}" "${agent_allocation_id}"
	close_agent_connection "${agent_config}"
	AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent stop --workspace "${workspace}" >/dev/null
	wait_agent_workspace_suspended "${agent_config}" "${second_service_id}" "${workspace}" "${codex_profile}"
	delete_json="$(AXERN_CONFIG="${agent_config}" "${AXERN_SMOKE_CMD[@]}" agent workspace delete -o json --workspace "${workspace}" --yes --timeout 180s)"
	second_claim_id="$(python3 -c 'import json,sys; data=json.load(sys.stdin); ids=data.get("claim_ids", []); print(ids[0] if ids else "")' <<<"${delete_json}")"
	if [ "${second_claim_id}" = "${first_claim_id}" ]; then
		echo "recreated agent workspace reused deleted claim identity" >&2
		return 1
	fi
	assert_agent_claim_directory_absent "${second_claim_id}"
	echo "tunnel_e2e_runtime=${runtime_class} agent_workspace=ok workspace=${workspace} deleted_service_id=${first_service_id} recreated_service_id=${second_service_id}"
}

verify_revoke_once() {
  local runtime_class="$1"
  local service_id="$2"
  local allocation_id="$3"
  local remote_port="$4"
  local tunnel_log="$5"
  local tunnel_session_id="$6"
  if [ "${verify_revoke}" != "true" ] || [ "${revoke_check_done}" = "true" ]; then
    return 0
  fi
  revoke_check_done=true
  echo "tunnel_e2e_runtime=${runtime_class} phase=revoke_convergence"
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel revoke -o json --reason e2e-revoke "${tunnel_session_id}" >/dev/null
  if ! wait_tunnel_unavailable "${runtime_class}" "${allocation_id}" "${remote_port}" $((SECONDS + 90)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
    return 1
  fi
  assert_tunnel_event "${tunnel_session_id}" "revoked"
  echo "tunnel_e2e_runtime=${runtime_class} revoked_unreachable=true"
}

verify_expire_once() {
  local runtime_class="$1"
  local service_id="$2"
  local allocation_id="$3"
  local local_port="$4"
  local expire_log expire_pid expire_session_id expire_remote_port expired_json
  if [ "${verify_expire}" != "true" ] || [ "${expire_check_done}" = "true" ]; then
    return 0
  fi
  expire_check_done=true
  echo "tunnel_e2e_runtime=${runtime_class} phase=expire_convergence"
  expire_log="${work_dir}/tunnel-expire-${runtime_class}.log"
  "${AXERN_SMOKE_CMD[@]}" tunnel open \
    --allocation-id "${allocation_id}" \
    --ttl 1m \
    --local "127.0.0.1:${local_port}" \
    >"${expire_log}" 2>&1 &
  expire_pid="$!"
  tunnel_pids+=("${expire_pid}")
  expire_session_id="$(wait_tunnel_session_id "${expire_log}")"
  expire_remote_port="$(wait_tunnel_remote_port "${expire_log}")"
  if ! wait_tunnel_response "${runtime_class}" "${allocation_id}" "${expire_remote_port}" "${expire_pid}" $((SECONDS + 90)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${expire_log}"
    return 1
  fi
  kill -STOP "${expire_pid}"
  sleep 85
  if ! wait_tunnel_unavailable "${runtime_class}" "${allocation_id}" "${expire_remote_port}" $((SECONDS + 60)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${expire_log}"
    return 1
  fi
  expired_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${expire_session_id}")"
  python3 -c '
import json
import sys

status = json.load(sys.stdin)["status"]
if status != "expired":
    raise SystemExit(f"session status = {status}, want expired")
' <<<"${expired_json}"
  assert_tunnel_event "${expire_session_id}" "expired"
  kill -CONT "${expire_pid}" >/dev/null 2>&1 || true
  kill "${expire_pid}" >/dev/null 2>&1 || true
  wait "${expire_pid}" >/dev/null 2>&1 || true
  echo "tunnel_e2e_runtime=${runtime_class} expired_unreachable=true session_id=${expire_session_id} remote_port=${expire_remote_port}"
}

verify_runtime() {
  local runtime_class="$1"
  local runtime_index="$2"
  local local_port="$3"
  local remote_port
  local namespace="tunnel-compose-e2e-${runtime_class}-$(date +%s)"
  local service_json service_id allocation_id tunnel_log tunnel_pid tunnel_session_id

  echo "tunnel_e2e_runtime=${runtime_class} phase=create_service"
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --template-id python311 --runtime-class "${runtime_class}" --replicas 1 \
    --argv python --argv -u --argv -m --argv http.server --argv 9000)"
  service_id="$(json_field 'data["service"]["id"]' <<<"${service_json}")"
  service_ids+=("${service_id}")

  allocation_id="$(wait_for_ready_allocation "${service_id}")"
  echo "tunnel_e2e_runtime=${runtime_class} allocation_id=${allocation_id}"

  tunnel_log="${work_dir}/tunnel-${runtime_class}.log"
  "${AXERN_SMOKE_CMD[@]}" service tunnel "${service_id}" \
    --allocation-id "${allocation_id}" \
    --ttl 1m \
    --to "127.0.0.1:${local_port}" \
    >"${tunnel_log}" 2>&1 &
  tunnel_pid="$!"
  tunnel_pids+=("${tunnel_pid}")
  tunnel_session_id="$(wait_tunnel_session_id "${tunnel_log}")"
  remote_port="$(wait_tunnel_remote_port "${tunnel_log}")"
  echo "tunnel_e2e_runtime=${runtime_class} session_id=${tunnel_session_id} allocated_remote_port=${remote_port}"
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${tunnel_session_id}" >/dev/null
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel list -o json --allocation-id "${allocation_id}" >/dev/null
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel inspect -o json "${tunnel_session_id}" >/dev/null
  assert_tunnel_event "${tunnel_session_id}" "created"

  if ! wait_tunnel_response "${runtime_class}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" $((SECONDS + 90)); then
    dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
    return 1
  fi
  runtime_results+=("${runtime_class}:${allocation_id}:${remote_port}")
  echo "tunnel_e2e_runtime=${runtime_class} result=ok allocation_id=${allocation_id} remote_port=${remote_port}"
  verify_renew_once "${runtime_class}" "${service_id}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" "${tunnel_log}" "${tunnel_session_id}"
  verify_relay_restart_once "${runtime_class}" "${service_id}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" "${tunnel_log}"
  verify_node_tunneld_restart_once "${runtime_class}" "${service_id}" "${allocation_id}" "${remote_port}" "${tunnel_pid}" "${tunnel_log}"
  if [ "${verify_agent}" = "true" ] && [ "${agent_check_done}" != "true" ]; then
    verify_agent_product_once "${runtime_class}" "${agent_upstream_port}"
  fi
  verify_revoke_once "${runtime_class}" "${service_id}" "${allocation_id}" "${remote_port}" "${tunnel_log}" "${tunnel_session_id}"
  verify_expire_once "${runtime_class}" "${service_id}" "${allocation_id}" "${local_port}"
}

if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

local_port="$(pick_local_port)"
start_upstream "${local_port}"
echo "tunnel_e2e_local_upstream=127.0.0.1:${local_port}"
agent_upstream_port="$(pick_local_port)"
if [ "${verify_agent}" = "true" ]; then
  start_agent_anthropic_upstream "${agent_upstream_port}"
  echo "tunnel_e2e_fake_agent_anthropic_upstream=127.0.0.1:${agent_upstream_port}"
fi

runtime_index=0
for runtime_class in ${runtime_list}; do
  verify_runtime "${runtime_class}" "${runtime_index}" "${local_port}"
  runtime_index=$((runtime_index + 1))
done

echo "tunnel_e2e_result=ok"
