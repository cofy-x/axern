#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
export K8S_GATEWAY_LOCAL_SSH_PORT="${K8S_GATEWAY_LOCAL_SSH_PORT:-25023}"

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock kind
trap 'cleanup; end_env_lock kind' EXIT

require_cmd curl
require_cmd kubectl
require_cmd python3

export KUBECONFIG="$(k8s_kubeconfig_file)"

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s
ensure_k8s_local_access

local_smoke_init_axern_cmd kind "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"

work_dir="$(mktemp -d)"
upstream_pid=""
service_ids=()
tunnel_pids=()
marker="axern-kind-tunnel-e2e-ok-$(date +%s)"
runtime_list="${AXERN_TUNNEL_E2E_RUNTIMES:-runsc runc}"

cleanup() {
  local pid service_id
  for pid in "${tunnel_pids[@]:-}"; do
    if [ -n "${pid}" ] && kill -0 "${pid}" >/dev/null 2>&1; then
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
  rm -rf "${work_dir}"
}

json_field() {
  local expr="$1"
  python3 -c "import json,sys; data=json.load(sys.stdin); print(${expr})"
}

assert_tunnel_session_targets() {
  local session_id="$1"
  local want_relay_id="$2"
  local want_client_target="$3"
  local want_node_target="$4"
  local session_json
  session_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${session_id}")"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
session = payload.get("session") or {}
want_relay, want_client, want_node = sys.argv[1:4]
got_relay = session.get("relayId") or ""
got_client = session.get("clientEdgeTarget") or session.get("edgeTarget") or ""
got_node = session.get("nodeEdgeTarget") or ""
if got_relay != want_relay:
    raise SystemExit(f"relayId={got_relay!r}, want {want_relay!r}")
if got_client != want_client:
    raise SystemExit(f"clientEdgeTarget={got_client!r}, want {want_client!r}")
if got_node != want_node:
    raise SystemExit(f"nodeEdgeTarget={got_node!r}, want {want_node!r}")
' "${want_relay_id}" "${want_client_target}" "${want_node_target}" <<<"${session_json}"
}

assert_tunnel_event() {
  local session_id="$1"
  local event_type="$2"
  local events_json
  events_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel events -o json --limit 30 "${session_id}")"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
want = sys.argv[1]
for event in payload.get("events", []):
    if event.get("eventType") == want:
        raise SystemExit(0)
raise SystemExit(f"missing tunnel event {want}")
' "${event_type}" <<<"${events_json}"
}

assert_tunnel_doctor() {
  local session_id="$1"
  local local_target="$2"
  local doctor_json
  doctor_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel doctor -o json --session-id "${session_id}" --local "${local_target}")"
  python3 -c '
import json
import sys

report = json.load(sys.stdin)
problems = report.get("problems") or []
if problems:
    raise SystemExit(f"doctor problems: {problems}")
if not report.get("control_reachable"):
    raise SystemExit("doctor control_reachable=false")
if not report.get("relay_reachable"):
    raise SystemExit("doctor relay_reachable=false")
if not report.get("local_reachable"):
    raise SystemExit("doctor local_reachable=false")
' <<<"${doctor_json}"
}

kind_node_pods() {
  kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
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
  local pod
  echo "kind tunnel e2e diagnostics runtime=${runtime_class} service=${service_id}" >&2
  echo "--- tunnel open log ---" >&2
  sed -n '1,220p' "${tunnel_log}" >&2 || true
  echo "--- service get ---" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" >&2 || true
  echo "--- service replicas ---" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service replicas -o json "${service_id}" >&2 || true
  for pod in $(kind_node_pods); do
    echo "--- ${pod} node-tunneld log ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc 'tail -n 160 /var/log/axnoded/node-tunneld.log 2>/dev/null || true' >&2 || true
    echo "--- ${pod} axnoded log ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc 'tail -n 120 /var/log/axnoded/axnoded.log 2>/dev/null || true' >&2 || true
  done
  echo "--- tunneld logs ---" >&2
  kubectl -n "${K8S_NAMESPACE}" logs -l app=tunneld --tail=160 >&2 || true
}

exec_in_allocation() {
  local runtime_class="$1"
  local allocation_id="$2"
  local remote_port="$3"
  local pod binary root_dir python_code output

  root_dir="/var/lib/axnoded/root/${runtime_class}"
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

  for pod in $(kind_node_pods); do
    binary="$(kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc "command -v ${runtime_class}" 2>/dev/null | tr -d '\r' || true)"
    [ -n "${binary}" ] || continue
    output="$(kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- "${binary}" --root "${root_dir}" exec "${allocation_id}" python -c "${python_code}" 2>/dev/null || true)"
    if [ -n "${output}" ]; then
      printf '%s\n' "${output}"
      return 0
    fi
  done
  return 1
}

wait_tunnel_remote_port() {
  local tunnel_log="$1"
  local deadline=$((SECONDS + 20))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local port
    port="$(python3 - "${tunnel_log}" <<'PY' || true
import pathlib
import re
import sys

text = pathlib.Path(sys.argv[1]).read_text(errors="ignore")
match = re.search(r"Remote bind: 127\.0\.0\.1:(\d+)", text)
if match:
    print(match.group(1))
PY
)"
    if [ -n "${port}" ]; then
      printf '%s\n' "${port}"
      return 0
    fi
    sleep 0.2
  done
  echo "tunnel open did not print an allocated remote port" >&2
  sed -n '1,80p' "${tunnel_log}" >&2 || true
  return 1
}

wait_tunnel_session_id() {
  local tunnel_log="$1"
  local deadline=$((SECONDS + 20))
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
    sleep 0.2
  done
  echo "tunnel open did not print a session id" >&2
  sed -n '1,80p' "${tunnel_log}" >&2 || true
  return 1
}

verify_runtime() {
  local runtime_class="$1"
  local runtime_index="$2"
  local local_port="$3"
  local remote_port
  local namespace="tunnel-kind-e2e-${runtime_class}-$(date +%s)"
  local service_json service_id allocation_id tunnel_log tunnel_pid tunnel_session_id observed

  echo "kind_tunnel_e2e_runtime=${runtime_class} phase=create_service"
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --template-id python311 --runtime-class "${runtime_class}" --replicas 1 \
    --argv python --argv -u --argv -m --argv http.server --argv 9000)"
  service_id="$(json_field 'data["service"]["id"]' <<<"${service_json}")"
  service_ids+=("${service_id}")

  allocation_id="$(wait_for_ready_allocation "${service_id}")"
  echo "kind_tunnel_e2e_runtime=${runtime_class} allocation_id=${allocation_id}"

  tunnel_log="${work_dir}/tunnel-${runtime_class}.log"
  "${AXERN_SMOKE_CMD[@]}" service tunnel "${service_id}" \
    --allocation-id "${allocation_id}" \
    --to "127.0.0.1:${local_port}" \
    >"${tunnel_log}" 2>&1 &
  tunnel_pid="$!"
  tunnel_pids+=("${tunnel_pid}")
  tunnel_session_id="$(wait_tunnel_session_id "${tunnel_log}")"
  remote_port="$(wait_tunnel_remote_port "${tunnel_log}")"
  echo "kind_tunnel_e2e_runtime=${runtime_class} session_id=${tunnel_session_id} allocated_remote_port=${remote_port}"
  assert_tunnel_session_targets \
    "${tunnel_session_id}" \
    "default" \
    "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" \
    "tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100"
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel list -o json --allocation-id "${allocation_id}" >/dev/null

  local deadline=$((SECONDS + 90))
  observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "tunnel process exited early for runtime=${runtime_class}" >&2
      dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
      return 1
    fi
    observed="$(exec_in_allocation "${runtime_class}" "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" = "${marker}" ]; then
      assert_tunnel_event "${tunnel_session_id}" "TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED"
      assert_tunnel_event "${tunnel_session_id}" "TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED"
      assert_tunnel_event "${tunnel_session_id}" "TUNNEL_SESSION_EVENT_TYPE_PAIRED"
      assert_tunnel_doctor "${tunnel_session_id}" "127.0.0.1:${local_port}"
      echo "kind_tunnel_e2e_runtime=${runtime_class} result=ok allocation_id=${allocation_id} remote_port=${remote_port}"
      return 0
    fi
    sleep 2
  done

  echo "unexpected tunnel response runtime=${runtime_class}: ${observed}" >&2
  dump_tunnel_diagnostics "${runtime_class}" "${service_id}" "${tunnel_log}"
  return 1
}

if [ -z "$(kind_node_pods)" ]; then
  echo "missing kind node-all-in-one pod; run make kind-up first" >&2
  exit 1
fi

local_port="$(pick_local_port)"
start_upstream "${local_port}"
echo "kind_tunnel_e2e_local_upstream=127.0.0.1:${local_port}"

runtime_index=0
for runtime_class in ${runtime_list}; do
  verify_runtime "${runtime_class}" "${runtime_index}" "${local_port}"
  runtime_index=$((runtime_index + 1))
done

echo "kind_tunnel_e2e_result=ok"
