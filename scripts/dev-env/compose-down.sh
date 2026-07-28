#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock compose
trap 'end_env_lock compose' EXIT

compose_project_down "${1:-}"

if [ "${1:-}" = "--purge" ]; then
  rm -rf "${COMPOSE_STATE_DIR}"
fi

echo "compose_down_ok=true"
