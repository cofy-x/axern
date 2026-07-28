#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock kind
trap 'end_env_lock kind' EXIT

export KUBECONFIG="$(k8s_kubeconfig_file)"

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s --quota-admission-smoke

echo "kind_quota_smoke_ok=true"
