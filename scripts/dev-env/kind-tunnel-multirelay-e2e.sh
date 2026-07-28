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
marker="axern-kind-tunnel-multirelay-e2e-ok-$(date +%s)"
created_service_id=""
opened_session_id=""
opened_remote_port=""
opened_tunnel_pid=""

default_registry="default,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100,1,false"
registry_a_active_b_drain=""
registry_a_drain_b_active=""

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
  kubectl -n "${K8S_NAMESPACE}" delete service/tunneld-a service/tunneld-b deployment/tunneld-a deployment/tunneld-b >/dev/null 2>&1 || true
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

apply_multirelay_topology() {
  kubectl apply -f - <<YAML >/dev/null
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunneld-a
  namespace: ${K8S_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tunneld-a
  template:
    metadata:
      labels:
        app: tunneld-a
    spec:
      containers:
        - name: tunneld
          image: axern/local-tunneld:dev
          imagePullPolicy: IfNotPresent
          args:
            - -listen
            - 0.0.0.0:24100
            - -control-target
            - controld.${K8S_NAMESPACE}.svc.cluster.local:24000
            - -tls-ca-cert
            - /certs/ca.crt
            - -tls-cert
            - /certs/client.crt
            - -tls-key
            - /certs/client.key
            - -relay-id
            - relay-a
            - -relay-tls-cert
            - /certs/controld.crt
            - -relay-tls-key
            - /certs/controld.key
          ports:
            - containerPort: 24100
          volumeMounts:
            - name: certs
              mountPath: /certs
              readOnly: true
      volumes:
        - name: certs
          secret:
            secretName: controld-pki
---
apiVersion: v1
kind: Service
metadata:
  name: tunneld-a
  namespace: ${K8S_NAMESPACE}
spec:
  selector:
    app: tunneld-a
  ports:
    - name: grpc
      port: 24100
      targetPort: 24100
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tunneld-b
  namespace: ${K8S_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tunneld-b
  template:
    metadata:
      labels:
        app: tunneld-b
    spec:
      containers:
        - name: tunneld
          image: axern/local-tunneld:dev
          imagePullPolicy: IfNotPresent
          args:
            - -listen
            - 0.0.0.0:24100
            - -control-target
            - controld.${K8S_NAMESPACE}.svc.cluster.local:24000
            - -tls-ca-cert
            - /certs/ca.crt
            - -tls-cert
            - /certs/client.crt
            - -tls-key
            - /certs/client.key
            - -relay-id
            - relay-b
            - -relay-tls-cert
            - /certs/controld.crt
            - -relay-tls-key
            - /certs/controld.key
          ports:
            - containerPort: 24100
          volumeMounts:
            - name: certs
              mountPath: /certs
              readOnly: true
      volumes:
        - name: certs
          secret:
            secretName: controld-pki
---
apiVersion: v1
kind: Service
metadata:
  name: tunneld-b
  namespace: ${K8S_NAMESPACE}
spec:
  selector:
    app: tunneld-b
  ports:
    - name: grpc
      port: 24100
      targetPort: 24100
YAML
  kubectl -n "${K8S_NAMESPACE}" rollout status deployment/tunneld-a --timeout=180s >/dev/null
  kubectl -n "${K8S_NAMESPACE}" rollout status deployment/tunneld-b --timeout=180s >/dev/null
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
    kubectl -n "${K8S_NAMESPACE}" set env deployment/controld CONTROLD_TUNNEL_RELAYS="${default_registry}" >/dev/null 2>&1 || true
    kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/controld >/dev/null 2>&1 || true
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/controld --timeout=180s >/dev/null 2>&1 || true
  fi
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

create_service() {
  local suffix="$1"
  local namespace="tunnel-kind-multirelay-e2e-${suffix}-$(date +%s)"
  local service_json service_id
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --template-id python311 --runtime-class runsc --replicas 1 \
    --argv python --argv -u --argv -m --argv http.server --argv 9000)"
  service_id="$(json_field 'data["service"]["id"]' <<<"${service_json}")"
  service_ids+=("${service_id}")
  created_service_id="${service_id}"
}

exec_in_allocation() {
  local allocation_id="$1"
  local remote_port="$2"
  local pod binary root_dir python_code output

  root_dir="/var/lib/axnoded/root/runsc"
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
    binary="$(kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc "command -v runsc" 2>/dev/null | tr -d '\r' || true)"
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

open_service_tunnel() {
  local label="$1"
  local service_id="$2"
  local allocation_id="$3"
  local local_port="$4"
  local tunnel_log="${work_dir}/tunnel-${label}.log"
  local tunnel_pid session_id remote_port

  "${AXERN_SMOKE_CMD[@]}" service tunnel "${service_id}" \
    --allocation-id "${allocation_id}" \
    --ttl 3m \
    --ready-timeout 45s \
    --to "127.0.0.1:${local_port}" \
    >"${tunnel_log}" 2>&1 &
  tunnel_pid="$!"
  tunnel_pids+=("${tunnel_pid}")
  session_id="$(wait_tunnel_log_match "${tunnel_log}" "Tunnel session:\\s*(\\S+)" 1 "session id")"
  remote_port="$(wait_tunnel_log_match "${tunnel_log}" "Remote bind: 127\\.0\\.0\\.1:(\\d+)" 1 "remote port")"
  opened_session_id="${session_id}"
  opened_remote_port="${remote_port}"
  opened_tunnel_pid="${tunnel_pid}"
}

assert_session_targets() {
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
relay_id = session.get("relayId")
client_target = session.get("clientEdgeTarget")
node_target = session.get("nodeEdgeTarget")
if relay_id != want_relay:
    raise SystemExit(f"relayId={relay_id!r}, want {want_relay!r}")
if client_target != want_client:
    raise SystemExit(f"clientEdgeTarget={client_target!r}, want {want_client!r}")
if node_target != want_node:
    raise SystemExit(f"nodeEdgeTarget={node_target!r}, want {want_node!r}")
' "${want_relay_id}" "${want_client_target}" "${want_node_target}" <<<"${session_json}"
}

wait_tunnel_response() {
  local allocation_id="$1"
  local remote_port="$2"
  local tunnel_pid="$3"
  local deadline="$4"
  local observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! kill -0 "${tunnel_pid}" >/dev/null 2>&1; then
      echo "tunnel process exited early" >&2
      return 2
    fi
    observed="$(exec_in_allocation "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" = "${marker}" ]; then
      return 0
    fi
    sleep 2
  done
  echo "unexpected tunnel response: ${observed}" >&2
  return 1
}

wait_tunnel_unavailable() {
  local allocation_id="$1"
  local remote_port="$2"
  local deadline="$3"
  local observed=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    observed="$(exec_in_allocation "${allocation_id}" "${remote_port}" 2>/dev/null || true)"
    if [ "${observed}" != "${marker}" ]; then
      return 0
    fi
    sleep 2
  done
  echo "tunnel still reachable allocation=${allocation_id} remote_port=${remote_port}" >&2
  return 1
}

assert_doctor_relay_unreachable() {
  local session_id="$1"
  local local_target="$2"
  local doctor_json
  doctor_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" tunnel doctor -o json --session-id "${session_id}" --local "${local_target}")"
  python3 -c '
import json
import sys

report = json.load(sys.stdin)
if report.get("relay_reachable"):
    raise SystemExit("doctor reported relay_reachable=true after bound relay was killed")
recommendation = (report.get("recommendation") or "").lower()
problems = " ".join(report.get("problems") or []).lower()
if "relay" not in recommendation and "relay" not in problems:
    raise SystemExit(f"doctor did not recommend relay action: {report}")
' <<<"${doctor_json}"
}

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s
ensure_k8s_local_access
local_smoke_init_axern_cmd kind "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"

if [ -z "$(kind_node_pods)" ]; then
  echo "missing kind node-all-in-one pod; run make kind-up first" >&2
  exit 1
fi

apply_multirelay_topology
registry_a_active_b_drain="relay-a,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld-a.${K8S_NAMESPACE}.svc.cluster.local:24100,100,false;relay-b,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld-b.${K8S_NAMESPACE}.svc.cluster.local:24100,1,true"
registry_a_drain_b_active="relay-a,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld-a.${K8S_NAMESPACE}.svc.cluster.local:24100,100,true;relay-b,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld-b.${K8S_NAMESPACE}.svc.cluster.local:24100,100,false"
local_port="$(pick_local_port)"
start_upstream "${local_port}"
echo "kind_tunnel_multirelay_e2e_local_upstream=127.0.0.1:${local_port}"

set_relay_registry "${registry_a_active_b_drain}"
create_service relay-a
service_a="${created_service_id}"
allocation_a="$(wait_for_ready_allocation "${service_a}")"
open_service_tunnel relay-a "${service_a}" "${allocation_a}" "${local_port}"
session_a="${opened_session_id}"
remote_port_a="${opened_remote_port}"
tunnel_pid_a="${opened_tunnel_pid}"
assert_session_targets "${session_a}" "relay-a" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "tunneld-a.${K8S_NAMESPACE}.svc.cluster.local:24100"
wait_tunnel_response "${allocation_a}" "${remote_port_a}" "${tunnel_pid_a}" $((SECONDS + 90))
echo "kind_tunnel_multirelay_e2e_phase=relay_a_bound session_id=${session_a} allocation_id=${allocation_a}"

set_relay_registry "${registry_a_drain_b_active}"
wait_tunnel_response "${allocation_a}" "${remote_port_a}" "${tunnel_pid_a}" $((SECONDS + 60))
echo "kind_tunnel_multirelay_e2e_existing_session_survived_drain=true"

create_service relay-b
service_b="${created_service_id}"
allocation_b="$(wait_for_ready_allocation "${service_b}")"
open_service_tunnel relay-b "${service_b}" "${allocation_b}" "${local_port}"
session_b="${opened_session_id}"
remote_port_b="${opened_remote_port}"
tunnel_pid_b="${opened_tunnel_pid}"
assert_session_targets "${session_b}" "relay-b" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "tunneld-b.${K8S_NAMESPACE}.svc.cluster.local:24100"
wait_tunnel_response "${allocation_b}" "${remote_port_b}" "${tunnel_pid_b}" $((SECONDS + 90))
echo "kind_tunnel_multirelay_e2e_phase=relay_b_bound session_id=${session_b} allocation_id=${allocation_b}"

kubectl -n "${K8S_NAMESPACE}" scale deployment/tunneld-a --replicas=0 >/dev/null
kubectl -n "${K8S_NAMESPACE}" rollout status deployment/tunneld-a --timeout=120s >/dev/null
wait_tunnel_unavailable "${allocation_a}" "${remote_port_a}" $((SECONDS + 90))
assert_doctor_relay_unreachable "${session_a}" "127.0.0.1:${local_port}"
echo "kind_tunnel_multirelay_e2e_bound_relay_kill_diagnosed=true"

echo "kind_tunnel_multirelay_e2e_result=ok"
