#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd kind
begin_env_lock kind
trap 'end_env_lock kind' EXIT

export KUBECONFIG="$(k8s_kubeconfig_file)"

if kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
  bash "${AXERN_ROOT}/scripts/dev-env/k8s-down.sh"
  kind delete cluster --name "${K8S_CLUSTER_NAME}" >/dev/null
fi

if [ "${1:-}" = "--purge" ]; then
  rm -rf "${K8S_STATE_DIR}"
fi

echo "kind_down_ok=true"
