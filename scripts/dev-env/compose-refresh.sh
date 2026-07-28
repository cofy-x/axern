#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

AXERN_COMPOSE_RESET_STATE="${AXERN_COMPOSE_RESET_STATE:-1}" \
  bash "${AXERN_ROOT}/scripts/dev-env/compose-up.sh"

echo "compose_refresh_ok=true"
