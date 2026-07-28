#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock compose
trap 'end_env_lock compose' EXIT

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose --run-smoke

echo "compose_run_smoke_ok=true"
