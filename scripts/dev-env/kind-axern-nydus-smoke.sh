#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock kind
trap 'end_env_lock kind' EXIT

require_cmd kubectl
require_cmd docker
require_cmd python3

export KUBECONFIG="$(k8s_kubeconfig_file)"
bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s

if [ -n "${NYDUS_TEST_IMAGE:-}" ]; then
  nydus_image="${NYDUS_TEST_IMAGE}"
else
  build_output="$(bash "${AXERN_ROOT}/scripts/dev-env/registry-nydus-image-build.sh")"
  nydus_image="$(printf '%s\n' "${build_output}" | awk -F= '$1 == "cluster_nydus_image" {print $2}')"
  if [ -z "${nydus_image}" ]; then
    printf '%s\n' "${build_output}" >&2
    echo "Nydus image build did not return cluster_nydus_image" >&2
    exit 1
  fi
fi
namespace="kind-axern-nydus-smoke-$(date +%s)"
runtime_class="${NYDUS_SMOKE_RUNTIME_CLASS:-runsc}"
run_id=""
run_json=""
environment_id=""
local_smoke_init_axern_cmd kind "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"

dump_nydus_smoke_diagnostics() {
  echo "--- nydus smoke diagnostics ---" >&2
  if [ -n "${run_id}" ]; then
    echo "--- run get ${run_id} ---" >&2
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run get -o json "${run_id}" 2>/dev/null | python3 -m json.tool >&2 || true
  fi
  echo "--- namespace runs ---" >&2
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run list --namespace "${namespace}" -o json 2>/dev/null | python3 -m json.tool >&2 || true
  echo "--- node-all-in-one pods ---" >&2
  kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o wide >&2 || true
  local pod
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    echo "--- ${pod}: imagemgr inventory ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- \
      curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null | python3 -m json.tool >&2 || true
    echo "--- ${pod}: imagemgr daemons ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- \
      curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/list_daemons 2>/dev/null | python3 -m json.tool >&2 || true
    echo "--- ${pod}: axctl image mounts ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- axctl image mounts --json >&2 || true
    echo "--- ${pod}: imagemgr log tail ---" >&2
    kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- tail -n 120 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
  done < <(kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
}

cleanup_nydus_smoke() {
  local rc=$?
  if [ "${rc}" -ne 0 ]; then
    dump_nydus_smoke_diagnostics || true
  fi
  if [ -n "${run_id}" ]; then
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json >/dev/null 2>&1 || true
    local_smoke_wait_for_run_terminal "${run_id}" >/dev/null 2>&1 || true
  fi
  if [ -n "${environment_id}" ]; then
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null 2>&1 || true
    environment_id=""
  fi
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null 2>&1 || true
  end_env_lock kind
  return "${rc}"
}
trap cleanup_nydus_smoke EXIT

create_output="$(
  local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" run create -o json \
      --namespace "${namespace}" \
      --image-ref "${nydus_image}" \
      --runtime-class "${runtime_class}" \
      --argv /bin/sh --argv -lc --argv 'python -c "print(\"nydus-rootfs-ok\")" && sleep 600'
)"
run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${create_output}")"
environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"].get("environment_id",""))' <<<"${create_output}")"
if ! run_json="$(local_smoke_wait_for_run_status "${run_id}" running)"; then
  printf '%s\n' "${run_json}" >&2
  echo "run ${run_id} did not reach running" >&2
  exit 1
fi
allocation_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"].get("allocation_id",""))' <<<"${run_json}")"

inventory_match=""
deadline=$((SECONDS + 90))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    body="$(kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null || true)"
    [ -n "${body}" ] || continue
    if python3 -c '
import json
import sys

image = sys.argv[1]
try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)

mounts = payload.get("mounted_images") or []
matched = any(
    item.get("mount_type") == "nydus"
    and (item.get("image_url") == image or item.get("nydus_image_url") == image)
    for item in mounts
)
if not matched:
    raise SystemExit(1)
if not isinstance(payload.get("chunkdb"), dict):
    raise SystemExit("inventory missing chunkdb object")
if not isinstance(payload.get("locality"), list):
    raise SystemExit("inventory missing locality list")
for key in ("chunkdb_error", "locality_error"):
    if payload.get(key):
        raise SystemExit(f"inventory reports {key}: {payload.get(key)}")
for item in payload.get("locality") or []:
    if (
        item.get("mount_type") == "nydus"
        and (item.get("image_url") == image or item.get("nydus_image_url") == image)
    ):
        break
else:
    raise SystemExit("inventory missing nydus locality entry")
' "${nydus_image}" <<<"${body}"
    then
      inventory_match="${pod}"
      break
    fi
  done < <(kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
  [ -z "${inventory_match}" ] || break
  sleep 2
done

if [ -z "${inventory_match}" ]; then
  echo "no node-all-in-one pod reported Nydus mount and readable imagefsd locality for ${nydus_image}" >&2
  exit 1
fi

local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json >/dev/null
local_smoke_wait_for_run_terminal "${run_id}" >/dev/null 2>&1 || true
run_id=""
if [ -n "${environment_id}" ]; then
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null
  environment_id=""
fi
local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null

echo "kind_axern_nydus_smoke_ok=true"
echo "nydus_image=${nydus_image}"
echo "runtime_class=${runtime_class}"
echo "allocation_id=${allocation_id}"
echo "imagemgr_pod=${inventory_match}"
