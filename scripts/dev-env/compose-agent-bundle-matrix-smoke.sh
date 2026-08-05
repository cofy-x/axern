#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

for base_image in ${AGENT_BUNDLE_COMPOSE_BASE_IMAGES:-busybox:1.36 ubuntu:24.04}; do
  if ! docker image inspect "${base_image}" >/dev/null 2>&1; then
    docker pull "${base_image}" >/dev/null
  fi
  LOCAL_CLAUDE_CODE_IMAGE_MOUNT_SMOKE_TASK_BASE_IMAGE="${base_image}" \
    bash "${AXERN_ROOT}/scripts/dev-env/compose-claude-code-image-mount-smoke.sh"
  LOCAL_CODEX_IMAGE_MOUNT_SMOKE_TASK_BASE_IMAGE="${base_image}" \
    bash "${AXERN_ROOT}/scripts/dev-env/compose-codex-image-mount-smoke.sh"
done

echo "agent_bundle_compose_matrix_smoke_ok=true"
