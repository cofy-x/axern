#!/usr/bin/env bash
set -euo pipefail

export AXNODED_CONTROL_PLANE_NODE_ID="${AXNODED_CONTROL_PLANE_NODE_ID:-node-verify}"
exec /usr/local/bin/node-all-in-one-entrypoint "$@"
