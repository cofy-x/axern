#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
export K8S_GATEWAY_LOCAL_SSH_PORT="${K8S_GATEWAY_LOCAL_SSH_PORT:-25023}"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

configure_kind_proxy

require_cmd kind
require_cmd kubectl
require_cmd curl
require_cmd docker
begin_env_lock kind
trap 'end_env_lock kind' EXIT

ensure_state_dirs
bash "${AXERN_ROOT}/scripts/dev-env/registry-up.sh" >/dev/null
ensure_kind_cluster

export KUBECONFIG="$(k8s_kubeconfig_file)"

ensure_host_image "${POSTGRES_IMAGE}"
ensure_host_image "${MINIO_IMAGE}"
load_image_to_cluster "${POSTGRES_IMAGE}"
load_image_to_cluster "${MINIO_IMAGE}"

bash "${AXERN_ROOT}/scripts/dev-env/k8s-up.sh"
IMAGE="${PYTHON311_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
IMAGE="${SERVER_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
IMAGE="${CODING_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
IMAGE="${CLAUDE_CODE_BUNDLE_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"
IMAGE="${CODEX_BUNDLE_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh"

echo "kind_up_ok=true"
echo "kubeconfig=$(k8s_kubeconfig_file)"
echo "axern_config=$(axern_config_file)"
emit_registry_status
