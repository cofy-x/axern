#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'cleanup; end_env_lock compose' EXIT

require_cmd curl
require_cmd docker
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
work_dir="$(mktemp -d)"
upstream_pid=""
tunnel_pid=""
service_id=""
allocation_id=""
tunnel_session_id=""
remote_port=""
result_json="${AXERN_TUNNEL_BENCHMARK_OUTPUT:-${AXERN_ROOT}/.dev/run/tunnel-benchmark-compose.json}"

cleanup() {
  set +e
  if [ -n "${tunnel_pid}" ] && kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
    kill "${tunnel_pid}" >/dev/null 2>&1 || true
    wait "${tunnel_pid}" >/dev/null 2>&1 || true
  fi
  if [ -n "${service_id}" ]; then
    local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
  fi
  if [ -n "${upstream_pid}" ] && kill -0 "${upstream_pid}" >/dev/null 2>&1; then
    kill "${upstream_pid}" >/dev/null 2>&1 || true
    wait "${upstream_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_dir}"
}

json_field() {
  local expr="$1"
  python3 -c "import json,sys; data=json.load(sys.stdin); print(${expr})"
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

start_benchmark_upstream() {
  local port="$1"
  python3 - "${port}" <<'PY' >/dev/null 2>&1 &
import time
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        query = urllib.parse.parse_qs(parsed.query)
        if parsed.path == "/health":
            payload = b"ok"
            self.send_response(200)
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        if parsed.path == "/bytes":
            size = int((query.get("n") or ["1048576"])[0])
            payload = b"x" * size
            self.send_response(200)
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        if parsed.path == "/stream":
            chunks = int((query.get("chunks") or ["64"])[0])
            size = int((query.get("size") or ["4096"])[0])
            delay_ms = int((query.get("delay_ms") or ["10"])[0])
            payload = b"s" * size
            self.send_response(200)
            self.send_header("content-type", "application/octet-stream")
            self.end_headers()
            for _ in range(chunks):
                self.wfile.write(payload)
                self.wfile.flush()
                time.sleep(delay_ms / 1000)
            return
        self.send_error(404, "unknown benchmark path")

    def log_message(self, *_):
        return

ThreadingHTTPServer(("127.0.0.1", int(__import__("sys").argv[1])), Handler).serve_forever()
PY
  upstream_pid="$!"

  local deadline=$((SECONDS + 10))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl --connect-timeout 1 --max-time 2 -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "benchmark upstream did not become ready on 127.0.0.1:${port}" >&2
  return 1
}

compose_runtime_binary() {
  local runtime_class="$1"
  docker exec "${node_container}" sh -lc "command -v ${runtime_class}" | tr -d '\r'
}

wait_for_ready_allocation() {
  local target_service_id="$1"
  local service_get replicas_json
  local deadline=$((SECONDS + 180))
  service_get=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${target_service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      replicas_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${target_service_id}")"
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

  echo "service did not become ready: ${target_service_id}" >&2
  printf '%s\n' "${service_get}" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${target_service_id}" >&2 || true
  return 1
}

wait_tunnel_log_match() {
  local tunnel_log="$1"
  local regex="$2"
  local group="$3"
  local label="$4"
  local deadline=$((SECONDS + 25))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local value
    value="$(python3 - "${tunnel_log}" "${regex}" "${group}" <<'PY' || true
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(sys.argv[2], text)
if match:
    print(match.group(int(sys.argv[3])))
PY
)"
    if [ -n "${value}" ]; then
      printf '%s\n' "${value}"
      return 0
    fi
    sleep 0.2
  done
  echo "tunnel command did not print ${label}" >&2
  sed -n '1,120p' "${tunnel_log}" >&2 || true
  return 1
}

exec_benchmark_python() {
  local code="$1"
  local binary root_dir
  binary="$(compose_runtime_binary runsc)"
  if [ -z "${binary}" ]; then
    echo "runsc binary not found in ${node_container}" >&2
    return 1
  fi
  root_dir="/var/lib/axnoded/root/runsc"
  docker exec "${node_container}" "${binary}" --root "${root_dir}" exec "${allocation_id}" python -c "${code}"
}

run_remote_scenario() {
  local name="$1"
  local code="$2"
  local output
  output="$(exec_benchmark_python "${code}")"
  python3 - "${work_dir}/results.jsonl" "${name}" "${output}" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
name = sys.argv[2]
payload = json.loads(sys.argv[3])
payload.setdefault("name", name)
path.write_text(path.read_text() + json.dumps(payload, sort_keys=True) + "\n" if path.exists() else json.dumps(payload, sort_keys=True) + "\n")
PY
}

run_relay_restart_scenario() {
  local started
  started="$(python3 - <<'PY'
import time
print(time.time())
PY
)"
  docker restart "${COMPOSE_PROJECT_NAME}-tunneld-1" >/dev/null
  local code
  code="$(cat <<PY
import json
import time
import urllib.request

url = "http://127.0.0.1:${remote_port}/health"
deadline = time.time() + 90
attempts = 0
while time.time() < deadline:
    attempts += 1
    try:
        with urllib.request.urlopen(url, timeout=3) as response:
            response.read()
        print(json.dumps({"name": "relay_restart_recovery", "attempts": attempts, "seconds": time.time() - float("${started}")}))
        raise SystemExit(0)
    except Exception:
        time.sleep(1)
raise SystemExit("tunnel did not recover after relay restart")
PY
)"
  run_remote_scenario relay_restart_recovery "${code}"
}

write_summary() {
  mkdir -p "$(dirname "${result_json}")"
  python3 - "${work_dir}/results.jsonl" "${result_json}" "${service_id}" "${allocation_id}" "${tunnel_session_id}" <<'PY'
import json
import pathlib
import sys
import time

jsonl = pathlib.Path(sys.argv[1])
output = pathlib.Path(sys.argv[2])
results = [json.loads(line) for line in jsonl.read_text().splitlines() if line.strip()]
payload = {
    "environment": "compose",
    "runtime_class": "runsc",
    "service_id": sys.argv[3],
    "allocation_id": sys.argv[4],
    "session_id": sys.argv[5],
    "created_at_unix": int(time.time()),
    "results": results,
}
output.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
print("tunnel_benchmark_compose_result=ok")
for result in results:
    name = result.get("name", "unknown")
    seconds = float(result.get("seconds") or 0)
    bytes_read = int(result.get("bytes") or result.get("total_bytes") or 0)
    if bytes_read and seconds:
        mibps = bytes_read / seconds / 1024 / 1024
        print(f"tunnel_benchmark_scenario={name} bytes={bytes_read} seconds={seconds:.3f} mib_per_second={mibps:.2f}")
    else:
        print(f"tunnel_benchmark_scenario={name} seconds={seconds:.3f}")
print(f"tunnel_benchmark_json={output}")
PY
}

if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

local_port="$(pick_local_port)"
start_benchmark_upstream "${local_port}"
echo "tunnel_benchmark_compose_local_upstream=127.0.0.1:${local_port}"

namespace="tunnel-compose-benchmark-$(date +%s)"
service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
  "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
  --template-id python311 --runtime-class runsc --replicas 1 \
  --argv python --argv -u --argv -m --argv http.server --argv 9000)"
service_id="$(json_field 'data["service"]["id"]' <<<"${service_json}")"
allocation_id="$(wait_for_ready_allocation "${service_id}")"

tunnel_log="${work_dir}/tunnel.log"
"${AXERN_SMOKE_CMD[@]}" service tunnel "${service_id}" \
  --allocation-id "${allocation_id}" \
  --ttl 5m \
  --ready-timeout 45s \
  --to "127.0.0.1:${local_port}" \
  >"${tunnel_log}" 2>&1 &
tunnel_pid="$!"
tunnel_session_id="$(wait_tunnel_log_match "${tunnel_log}" "Tunnel session:\\s*(\\S+)" 1 "session id")"
remote_port="$(wait_tunnel_log_match "${tunnel_log}" "Remote bind: 127\\.0\\.0\\.1:(\\d+)" 1 "remote port")"

printf '' >"${work_dir}/results.jsonl"

run_remote_scenario single_stream "$(cat <<PY
import json
import time
import urllib.request

url = "http://127.0.0.1:${remote_port}/bytes?n=4194304"
start = time.time()
with urllib.request.urlopen(url, timeout=30) as response:
    data = response.read()
print(json.dumps({"name": "single_stream", "bytes": len(data), "seconds": time.time() - start}))
PY
)"

run_remote_scenario concurrent_streams "$(cat <<PY
import concurrent.futures
import json
import time
import urllib.request

url = "http://127.0.0.1:${remote_port}/bytes?n=524288"
def fetch(_):
    with urllib.request.urlopen(url, timeout=30) as response:
        return len(response.read())
start = time.time()
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
    sizes = list(executor.map(fetch, range(8)))
print(json.dumps({"name": "concurrent_streams", "streams": len(sizes), "total_bytes": sum(sizes), "seconds": time.time() - start}))
PY
)"

run_remote_scenario long_streaming_response "$(cat <<PY
import json
import time
import urllib.request

url = "http://127.0.0.1:${remote_port}/stream?chunks=40&size=4096&delay_ms=20"
start = time.time()
with urllib.request.urlopen(url, timeout=30) as response:
    data = response.read()
print(json.dumps({"name": "long_streaming_response", "bytes": len(data), "seconds": time.time() - start}))
PY
)"

run_remote_scenario slow_consumer "$(cat <<PY
import json
import time
import urllib.request

url = "http://127.0.0.1:${remote_port}/bytes?n=524288"
total = 0
start = time.time()
with urllib.request.urlopen(url, timeout=30) as response:
    while True:
        chunk = response.read(2048)
        if not chunk:
            break
        total += len(chunk)
        time.sleep(0.003)
print(json.dumps({"name": "slow_consumer", "bytes": total, "seconds": time.time() - start}))
PY
)"

run_relay_restart_scenario
write_summary
