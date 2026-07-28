#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock kind
trap 'end_env_lock kind' EXIT

bash "${AXERN_ROOT}/scripts/dev-env/kind-down.sh" --purge

echo "kind_purge_ok=true"
