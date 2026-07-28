#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock kind
trap 'end_env_lock kind' EXIT

require_cmd curl
require_cmd docker
require_cmd kubectl
require_cmd python3

export KUBECONFIG="$(k8s_kubeconfig_file)"
bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s
run_local_image_service_smoke kind "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" kind-image-service "http://127.0.0.1:${K8S_GATEWAY_LOCAL_HTTP_PORT}"
