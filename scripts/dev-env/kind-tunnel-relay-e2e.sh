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

work_dir="$(mktemp -d)"
upstream_pid=""
service_ids=()
tunnel_pids=()
marker="axern-kind-tunnel-relay-e2e-ok-$(date +%s)"
verified_allocation_id=""

active_registry="default,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100,1,false"
mixed_registry="drain,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100,100,true;${active_registry}"
drain_only_registry="default,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100,1,true"

cleanup() {
  set +e
  restore_relay_registry
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

set_relay_registry() {
  local registry="$1"
  kubectl -n "${K8S_NAMESPACE}" set env deployment/controld CONTROLD_TUNNEL_RELAYS="${registry}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/controld >/dev/null
  kubectl -n "${K8S_NAMESPACE}" rollout status deployment/controld --timeout=180s >/dev/null
  bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s >/dev/null
}

restore_relay_registry() {
  if kubectl -n "${K8S_NAMESPACE}" get deployment/controld >/dev/null 2>&1; then
    kubectl -n "${K8S_NAMESPACE}" set env deployment/controld CONTROLD_TUNNEL_RELAYS="${active_registry}" >/dev/null 2>&1 || true
    kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/controld >/dev/null 2>&1 || true
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/controld --timeout=180s >/dev/null 2>&1 || true
  fi
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

kind_node_pods() {
  kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
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

wait_tunnel_log_match() {
  local tunnel_log="$1"
  local regex="$2"
  local group="$3"
  local label="$4"
  local deadline=$((SECONDS + 20))
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
  echo "tunnel open did not print ${label}" >&2
  sed -n '1,80p' "${tunnel_log}" >&2 || true
  return 1
}

assert_session_registry() {
  local session_id="$1"
  local session_json
  session_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel get -o json "${session_id}")"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
session = payload.get("session") or {}
want_client = f"127.0.0.1:{sys.argv[1]}"
want_node = f"tunneld.{sys.argv[2]}.svc.cluster.local:24100"
if session.get("relayId") != "default":
    raise SystemExit("session selected relay %r, want default" % (session.get("relayId"),))
if session.get("clientEdgeTarget") != want_client:
    raise SystemExit("client target %r, want %r" % (session.get("clientEdgeTarget"), want_client))
if session.get("nodeEdgeTarget") != want_node:
    raise SystemExit("node target %r, want %r" % (session.get("nodeEdgeTarget"), want_node))
' "${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_NAMESPACE}" <<<"${session_json}"
}

assert_event() {
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

verify_drain_is_skipped() {
  local local_port="$1"
  local namespace="tunnel-kind-relay-e2e-$(date +%s)"
  local service_json service_id allocation_id tunnel_log tunnel_pid session_id remote_port observed

  echo "kind_tunnel_relay_e2e_phase=drain_skip"
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --template-id python311 --runtime-class runsc --replicas 1 \
    --argv python --argv -u --argv -m --argv http.server --argv 9000)"
  service_id="$(json_field 'data["service"]["id"]' <<<"${service_json}")"
  service_ids+=("${service_id}")
  allocation_id="$(wait_for_ready_allocation "${service_id}")"
  verified_allocation_id="${allocation_id}"

  tunnel_log="${work_dir}/tunnel-drain-skip.log"
  "${AXERN_SMOKE_CMD[@]}" tunnel open \
    --allocation-id "${allocation_id}" \
    --local "127.0.0.1:${local_port}" \
    >"${tunnel_log}" 2>&1 &
  tunnel_pid="$!"
  tunnel_pids+=("${tunnel_pid}")
  session_id="$(wait_tunnel_log_match "${tunnel_log}" "Tunnel session (\\S+) readying" 1 "session id")"
  remote_port="$(wait_tunnel_log_match "${tunnel_log}" "Remote bind: 127\\.0\\.0\\.1:(\\d+)" 1 "remote port")"

  assert_session_registry "${session_id}"

  local deadline=$((SECONDS + 90))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    observed="$(exec_in_allocation runsc "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" = "${marker}" ]; then
      assert_event "${session_id}" "TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED"
      assert_event "${session_id}" "TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED"
      assert_event "${session_id}" "TUNNEL_SESSION_EVENT_TYPE_PAIRED"
      echo "kind_tunnel_relay_e2e_drain_skip=ok session_id=${session_id}"
      return 0
    fi
    sleep 2
  done
  echo "drain skip tunnel did not forward marker; observed=${observed}" >&2
  sed -n '1,120p' "${tunnel_log}" >&2 || true
  return 1
}

verify_all_drain_rejects_create() {
  local local_port="$1"
  local allocation_id="$2"
  local fail_log="${work_dir}/tunnel-drain-reject.log"

  echo "kind_tunnel_relay_e2e_phase=drain_reject"
  if "${AXERN_SMOKE_CMD[@]}" tunnel open \
    --allocation-id "${allocation_id}" \
    --local "127.0.0.1:${local_port}" \
    >"${fail_log}" 2>&1; then
    echo "tunnel open unexpectedly succeeded while every relay was draining" >&2
    sed -n '1,120p' "${fail_log}" >&2 || true
    return 1
  fi
  if ! grep -q "no non-draining tunnel relays are available" "${fail_log}"; then
    echo "tunnel open failed with an unexpected error while every relay was draining" >&2
    sed -n '1,120p' "${fail_log}" >&2 || true
    return 1
  fi
  echo "kind_tunnel_relay_e2e_drain_reject=ok"
}

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s
ensure_k8s_local_access
local_smoke_init_axern_cmd kind "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"

if [ -z "$(kind_node_pods)" ]; then
  echo "missing kind node-all-in-one pod; run make kind-up first" >&2
  exit 1
fi

local_port="$(pick_local_port)"
start_upstream "${local_port}"
echo "kind_tunnel_relay_e2e_local_upstream=127.0.0.1:${local_port}"

set_relay_registry "${mixed_registry}"
verify_drain_is_skipped "${local_port}"

set_relay_registry "${drain_only_registry}"
verify_all_drain_rejects_create "${local_port}" "${verified_allocation_id}"

restore_relay_registry
echo "kind_tunnel_relay_e2e_result=ok"
