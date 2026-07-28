#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker

if docker inspect "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1; then
  docker stop "${LOCAL_REGISTRY_NAME}" >/dev/null
fi

echo "registry_down_ok=true"
emit_registry_status
