#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd kind
require_cmd docker

if kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
  echo "kind_cluster=present"
else
  echo "kind_cluster=absent"
  exit 0
fi

export KUBECONFIG="$(k8s_kubeconfig_file)"

emit_lock_status "env-kind"
echo "kubeconfig=${KUBECONFIG}"
emit_registry_status
kubectl get nodes -o wide
AXERN_STATUS_SUPPRESS_LOCK_STATUS=1 bash "${AXERN_ROOT}/scripts/dev-env/k8s-status.sh"
emit_kind_imported_image_status
