#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose --quota-admission-smoke

echo "compose_quota_smoke_ok=true"
