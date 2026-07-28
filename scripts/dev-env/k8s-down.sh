#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock "${K8S_ENV_NAME}"
trap 'end_env_lock "${K8S_ENV_NAME}"' EXIT

kubectl delete namespace "${K8S_NAMESPACE}" --ignore-not-found=true >/dev/null 2>&1 || true

if [ "${1:-}" = "--purge" ]; then
  rm -rf "${K8S_STATE_DIR}"
fi

echo "k8s_down_ok=true"
