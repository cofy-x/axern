#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
export K8S_GATEWAY_LOCAL_SSH_PORT="${K8S_GATEWAY_LOCAL_SSH_PORT:-25023}"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

configure_kind_proxy_from_host

require_cmd kind
require_cmd kubectl
require_cmd curl
require_cmd docker

if ! kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
  echo "kind cluster is not present: ${K8S_CLUSTER_NAME}; run make kind-up first" >&2
  exit 1
fi

export KUBECONFIG="$(k8s_kubeconfig_file)"
kind export kubeconfig --name "${K8S_CLUSTER_NAME}" --kubeconfig "${KUBECONFIG}" >/dev/null

AXERN_K8S_RESET_POSTGRES="${AXERN_K8S_RESET_POSTGRES:-1}" \
  bash "${AXERN_ROOT}/scripts/dev-env/k8s-up.sh"

if [ "${AXERN_KIND_REFRESH_IMPORT_RUNTIME_IMAGES:-1}" = "1" ] || [ "${AXERN_KIND_REFRESH_IMPORT_RUNTIME_IMAGES:-1}" = "true" ]; then
  for image_ref in "${PYTHON311_RUNTIME_IMAGE}" "${SERVER_BASE_RUNTIME_IMAGE}" "${CODING_BASE_RUNTIME_IMAGE}" "${DESKTOP_BASE_RUNTIME_IMAGE}" "${CLAUDE_CODE_BUNDLE_IMAGE}" "${CODEX_BUNDLE_IMAGE}"; do
    if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
      echo "missing runtime image ${image_ref}; run make kind-up or make local-images-build once before kind-refresh" >&2
      exit 1
    fi
    IMAGE="${image_ref}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
  done
fi

echo "kind_refresh_ok=true"
echo "kubeconfig=$(k8s_kubeconfig_file)"
echo "axern_config=$(axern_config_file)"
