#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
tag="$(axern_release_version)"
for image in controld tunneld gatewayd node-all-in-one python311-runtime server-base-runtime coding-base-runtime desktop-base-runtime claude-code-bundle codex-bundle; do
  target="${AXERN_RELEASE_REGISTRY}/${image}:${tag}"
  docker buildx imagetools create --tag "${target}" "${target}-amd64" "${target}-arm64"
  docker buildx imagetools inspect "${target}"
done
