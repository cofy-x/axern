#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

ensure_state_dirs
sync_local_axern_contexts

config_file="$(axern_config_file)"
echo "axern_config_init_ok=true"
echo "axern_config=${config_file}"

