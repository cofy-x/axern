#!/usr/bin/env bash
set -euo pipefail

DEMO_CONTAINER_NAME="${DEMO_CONTAINER_NAME:-axnoded-dashboard-nginx-demo}"
docker rm -f "${DEMO_CONTAINER_NAME}" >/dev/null 2>&1 || true
