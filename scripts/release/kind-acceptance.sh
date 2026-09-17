#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
# shellcheck source=../proxy-env.sh
source "${AXERN_ROOT}/scripts/proxy-env.sh"
tag="$(axern_release_version)"
namespace="axern-release"
cluster="axern-release"
chart="${AXERN_HELM_CHART:-oci://ghcr.io/cofy-x/charts/axern}"
image_tag_suffix="${AXERN_RELEASE_IMAGE_TAG_SUFFIX:-}"
# This disposable kind cluster exercises the required cgroup path with an
# explicit non-production reserve. The reserve must cover both the 512 MiB
# runtime-conformance charge and axnoded's internal cgroup; production values
# still come from a qualification receipt.
release_test_minimum_memory_system_reserve_bytes=1073741824
release_test_memory_system_reserve_bytes="${AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES:-1073741824}"
if ! [[ "${release_test_memory_system_reserve_bytes}" =~ ^[1-9][0-9]*$ ]]; then
  echo "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES must be a positive decimal integer" >&2
  exit 1
fi
if ((release_test_memory_system_reserve_bytes < release_test_minimum_memory_system_reserve_bytes)); then
  echo "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES must reserve at least 1073741824 bytes for runtime conformance and axnoded headroom" >&2
  exit 1
fi
capability_ready_timeout_seconds="${AXERN_RELEASE_CAPABILITY_READY_TIMEOUT_SECONDS:-300}"
if ! [[ "${capability_ready_timeout_seconds}" =~ ^[1-9][0-9]*$ ]]; then
  echo "AXERN_RELEASE_CAPABILITY_READY_TIMEOUT_SECONDS must be a positive decimal integer" >&2
  exit 1
fi
state_dir="$(mktemp -d)"
cleanup() {
  local status=$?
  if ((status != 0)); then
    echo "--- release acceptance diagnostics ---" >&2
    kubectl --namespace "${namespace}" get pods -o wide >&2 || true
    if [[ -n "${config:-}" && -f "${config}" && -n "${cli:-}" ]]; then
      echo "--- allocation lifecycle retries ---" >&2
      "${cli}" --config "${config}" admin allocation-retry list --output json >&2 || true
    fi
    echo "--- controld logs ---" >&2
    kubectl --namespace "${namespace}" logs deployment/controld --all-containers --tail=500 >&2 || true
    echo "--- gatewayd logs ---" >&2
    kubectl --namespace "${namespace}" logs deployment/gatewayd --all-containers --tail=300 >&2 || true
    echo "--- node container logs ---" >&2
    kubectl --namespace "${namespace}" logs -l app.kubernetes.io/component=node --all-containers --tail=500 >&2 || true
    echo "--- axnoded file log ---" >&2
    kubectl --namespace "${namespace}" exec "$(kubectl --namespace "${namespace}" get pod -l app.kubernetes.io/component=node -o jsonpath='{.items[0].metadata.name}')" -- \
      sh -c 'tail -n 1000 /var/log/axnoded/axnoded.log' >&2 || true
  fi
  jobs -p | xargs kill >/dev/null 2>&1 || true
  kind delete cluster --name "${cluster}" >/dev/null 2>&1 || true
  rm -rf "${state_dir}"
  return "${status}"
}
trap cleanup EXIT

release_http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
release_https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
release_no_proxy="$(append_no_proxy_entries \
  "${NO_PROXY:-${no_proxy:-}}" \
  'localhost,127.0.0.1,::1,host.docker.internal,.svc,.svc.cluster.local,.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16')"
if [ -n "${release_http_proxy}" ] || [ -n "${release_https_proxy}" ]; then
  export HTTP_PROXY="${release_http_proxy}"
  export HTTPS_PROXY="${release_https_proxy}"
  export NO_PROXY="${release_no_proxy}"
  export http_proxy="${release_http_proxy}"
  export https_proxy="${release_https_proxy}"
  export no_proxy="${release_no_proxy}"
  echo "release_kind_proxy_configured=true"
fi

kind create cluster --name "${cluster}" --wait 120s
kubectl create namespace "${namespace}"
cli="${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}"
"${cli}" admin pki bootstrap --directory "${state_dir}/pki" --cluster axern.local --dns localhost,controld,gatewayd,tunneld
kubectl --namespace "${namespace}" create secret generic axern-pki \
  --from-file=ca.crt="${state_dir}/pki/ca.crt" \
  --from-file=controld.pem="${state_dir}/pki/controld.pem" \
  --from-file=gatewayd.pem="${state_dir}/pki/gatewayd.pem" \
  --from-file=tunneld.pem="${state_dir}/pki/tunneld.pem" \
  --from-file=client.crt="${state_dir}/pki/client.crt" \
  --from-file=client.key="${state_dir}/pki/client.key"
kubectl --namespace "${namespace}" create secret generic axern-pki-signer \
  --from-file=signer.pem="${state_dir}/pki/private/signer.pem"
release_node_name="$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')"
release_node_id="node-$(openssl rand -hex 16)"
release_enrollment_token_file="${state_dir}/enrollment-token"
openssl rand -hex 32 > "${release_enrollment_token_file}"
chmod 600 "${release_enrollment_token_file}"
kubectl --namespace "${namespace}" create secret generic enrollment-token \
  --from-file="${release_node_id}=${release_enrollment_token_file}"

helm_args=(
  install axern "${chart}"
  --namespace "${namespace}"
  --wait --timeout 15m
  --set-string "node.memorySystemReserveBytes=${release_test_memory_system_reserve_bytes}"
  --set-string "node.enrollment.existingSecret=enrollment-token"
  --set-string "node.enrollment.nodes[0].nodeName=${release_node_name}"
  --set-string "node.enrollment.nodes[0].nodeID=${release_node_id}"
)
if [[ "${chart}" == oci://* ]]; then
  helm_args+=(--version "${tag#v}")
fi
if [ -n "${image_tag_suffix}" ]; then
  candidate_tag="${tag}-${image_tag_suffix}"
  helm_args+=(
    --set-string "controld.image.tag=${candidate_tag}"
    --set-string "tunneld.image.tag=${candidate_tag}"
    --set-string "node.image.tag=${candidate_tag}"
    --set-string "gatewayd.image.tag=${candidate_tag}"
    --set-string "runtimeCatalog.python311Image=${AXERN_RELEASE_REGISTRY}/python311-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.serverBaseImage=${AXERN_RELEASE_REGISTRY}/server-base-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.codingBaseImage=${AXERN_RELEASE_REGISTRY}/coding-base-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.desktopBaseImage=${AXERN_RELEASE_REGISTRY}/desktop-base-runtime:${candidate_tag}"
  )
fi
if [ -n "${release_http_proxy}" ] || [ -n "${release_https_proxy}" ]; then
  registry_proxy_url="${release_https_proxy:-${release_http_proxy}}"
  helm_no_proxy="${release_no_proxy//,/\\,}"
  helm_args+=(
    --set-string "proxyEnv.HTTP_PROXY=${release_http_proxy}"
    --set-string "proxyEnv.HTTPS_PROXY=${release_https_proxy}"
    --set-string "proxyEnv.NO_PROXY=${helm_no_proxy}"
    --set-string "proxyEnv.http_proxy=${release_http_proxy}"
    --set-string "proxyEnv.https_proxy=${release_https_proxy}"
    --set-string "proxyEnv.no_proxy=${helm_no_proxy}"
    --set-string "proxyEnv.REGISTRY_PROXY_URL=${registry_proxy_url}"
    --set-string "proxyEnv.REGISTRY_NO_PROXY=${helm_no_proxy}"
  )
fi
if [ -n "${AXERN_REGISTRY_USERNAME:-}" ] && [ -n "${AXERN_REGISTRY_PASSWORD:-}" ]; then
  kubectl --namespace "${namespace}" create secret docker-registry axern-release-registry \
    --docker-server="${AXERN_RELEASE_REGISTRY%%/*}" \
    --docker-username="${AXERN_REGISTRY_USERNAME}" \
    --docker-password="${AXERN_REGISTRY_PASSWORD}"
  helm_args+=(
    --set-string 'global.imagePullSecrets[0].name=axern-release-registry'
    --set-string 'node.registryAuth.existingSecret=axern-release-registry'
  )
fi
helm "${helm_args[@]}"
kubectl --namespace "${namespace}" rollout status deployment/controld --timeout=5m
kubectl --namespace "${namespace}" rollout status deployment/gatewayd --timeout=5m
kubectl --namespace "${namespace}" rollout status daemonset -l app.kubernetes.io/component=node --timeout=10m

kubectl --namespace "${namespace}" port-forward svc/gatewayd 25100:25000 25101:25080 >"${state_dir}/port-forward.log" 2>&1 &
port_forward_pid=$!
gateway_ready=false
for _ in $(seq 1 60); do
  if curl --fail --silent http://127.0.0.1:25101/healthz >/dev/null 2>&1; then
    gateway_ready=true
    break
  fi
  if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
    cat "${state_dir}/port-forward.log" >&2
    exit 1
  fi
  sleep 1
done
if [ "${gateway_ready}" != "true" ]; then
  cat "${state_dir}/port-forward.log" >&2
  echo "gateway port-forward did not become ready" >&2
  exit 1
fi

config="${state_dir}/config.json"
cli="${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}"
"${cli}" --config "${config}" context import-kubernetes release \
  --namespace "${namespace}" --cert-dir "${state_dir}/certs" --current
"${cli}" --config "${config}" admin node admit "${release_node_id}" \
  --enrollment-token-file "${release_enrollment_token_file}" \
  --operator-reason "release acceptance node admission"
"${cli}" --config "${config}" namespace create default --output json
"${cli}" --config "${config}" doctor --namespace default --output json

wait_for_release_capabilities() {
  local deadline node_id
  local nodes_json="${state_dir}/nodes.json"
  local snapshot_json="${state_dir}/capability-snapshot.json"
  deadline=$((SECONDS + capability_ready_timeout_seconds))

  while ((SECONDS < deadline)); do
    if "${cli}" --config "${config}" admin node list --status active --output json >"${nodes_json}" 2>/dev/null; then
      while IFS= read -r node_id; do
        if "${cli}" --config "${config}" admin node capability snapshot "${node_id}" --output json >"${snapshot_json}" 2>/dev/null &&
          python3 - "${snapshot_json}" <<'PY'
import json
import pathlib
import re
import sys
from datetime import datetime, timezone

data = json.loads(pathlib.Path(sys.argv[1]).read_text())
now = datetime.now(timezone.utc)

def is_available(item):
    if (
        item.get("state") != "CAPABILITY_STATE_AVAILABLE"
        or item.get("reasonCode") != "CAPABILITY_REASON_CODE_AVAILABLE"
    ):
        return False
    valid_until = item.get("validUntil")
    if not valid_until:
        return True
    # Protobuf JSON timestamps may carry nanoseconds, while some supported
    # host Python versions accept at most microseconds in fromisoformat().
    # Truncation is sufficient for this readiness deadline comparison and
    # keeps the release harness portable without weakening the expiry check.
    normalized = re.sub(r"(\.\d{6})\d+", r"\1", valid_until)
    return datetime.fromisoformat(normalized.replace("Z", "+00:00")) > now

available = {
    item.get("key", {}).get("platform")
    for item in data.get("snapshot", {}).get("observations", [])
    if is_available(item)
}
network = {
    "PLATFORM_CAPABILITY_NETWORK_BRIDGE",
    "PLATFORM_CAPABILITY_NETWORK_BPFNET",
}
storage = "PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT"
raise SystemExit(0 if storage in available and not network.isdisjoint(available) else 1)
PY
        then
          echo "release_kind_node_ready=${node_id}"
          return 0
        fi
      done < <(python3 - "${nodes_json}" <<'PY'
import json
import pathlib
import sys

data = json.loads(pathlib.Path(sys.argv[1]).read_text())
for node in data.get("nodes", []):
    if (
        node.get("lifecycle_status") == "active"
        and node.get("heartbeat_fresh") is True
        and node.get("summary_fresh") is True
        and node.get("axnoded_ready") is True
        and node.get("node_id")
    ):
        print(node["node_id"])
PY
      )
    fi
    sleep 2
  done

  echo "timed out waiting for a release node with required network and runsc storage capabilities" >&2
  if [[ -s "${nodes_json}" ]]; then
    echo "--- admin nodes ---" >&2
    cat "${nodes_json}" >&2
  fi
  if [[ -s "${snapshot_json}" ]]; then
    echo "--- latest capability snapshot ---" >&2
    cat "${snapshot_json}" >&2
  fi
  echo "--- node pods ---" >&2
  kubectl --namespace "${namespace}" get pods -o wide >&2 || true
  echo "--- node logs ---" >&2
  kubectl --namespace "${namespace}" logs -l app.kubernetes.io/component=node --all-containers --tail=300 >&2 || true
  echo "--- axnoded file log ---" >&2
  kubectl --namespace "${namespace}" exec "$(kubectl --namespace "${namespace}" get pod -l app.kubernetes.io/component=node -o jsonpath='{.items[0].metadata.name}')" -- \
    sh -c 'tail -n 500 /var/log/axnoded/axnoded.log' >&2 || true
  return 1
}

wait_for_release_capabilities

cat >"${state_dir}/run.yaml" <<'YAML'
api_version: axern/v1
kind: Run
metadata:
  namespace: default
spec:
  source:
    template: python311
  command:
    argv: [python, -c, "print('axern-release-ok')"]
  resources:
    requests:
      cpu: 100m
      memory: 512MiB
YAML
"${cli}" --config "${config}" --timeout 15m run --wait-timeout 15m --file "${state_dir}/run.yaml"

if [ "$#" -gt 0 ]; then
  sdk_acceptance_image="${AXERN_RELEASE_REGISTRY}/python311-runtime:${tag}${image_tag_suffix:+-${image_tag_suffix}}"
  AXERN_SDK_ACCEPTANCE_CONFIG="${config}" \
    AXERN_SDK_ACCEPTANCE_CONTEXT=release \
    AXERN_SDK_ACCEPTANCE_CLI="${cli}" \
    AXERN_SDK_ACCEPTANCE_IMAGE_MOUNT="${sdk_acceptance_image}" \
    "$@"
fi

echo "release_kind_acceptance_ok=${tag}${image_tag_suffix:+-${image_tag_suffix}}"
