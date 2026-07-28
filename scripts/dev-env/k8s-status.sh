#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

if [ "${AXERN_STATUS_SUPPRESS_LOCK_STATUS:-0}" != "1" ]; then
  emit_lock_status "env-${K8S_ENV_NAME}"
fi

kubectl -n "${K8S_NAMESPACE}" get pods,svc,daemonset,deployment 2>/dev/null || true

proxy_config_json="$(kubectl -n "${K8S_NAMESPACE}" get configmap local-proxy-env -o json 2>/dev/null || true)"
emit_k8s_proxy_status "${proxy_config_json}"

local_access_state="stopped"
if curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1; then
  if [ "${K8S_ENV_NAME}" = "kind" ]; then
    local_access_state="nodeport"
  else
    local_access_state="reachable"
  fi
fi
echo "local_access=${local_access_state}"

if curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1; then
  echo "controld_health=ready"
  emit_node_summary_status "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/nodesz"
else
  echo "controld_health=unreachable"
  echo "node_count=0"
  echo "node_summary_fresh=false"
  echo "axnoded_ready=false"
  echo "interface_pool=0/0/0"
  echo "cgroup_pool=0/0/0"
  echo "running_allocation_ids=0"
  echo "active_allocation_ids=0"
  echo "running_containers=0"
  echo "mounted_images=0"
  echo "imagemgr_ready_nodes=0"
  echo "imagefsd_ready_nodes=0"
  echo "volumed_ready_nodes=0"
  echo "volumed_error_nodes=0"
fi
