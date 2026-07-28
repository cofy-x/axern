#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
arch="${1:-}"
case "${arch}" in
  amd64|arm64) ;;
  *) echo "usage: $0 <amd64|arm64>" >&2; exit 2 ;;
esac
base_tag="$(axern_release_version)"
export AXERN_RELEASE_VERSION="${base_tag}-${arch}"
axern_export_release_images
export AXERN_TARGET_GOARCH="${arch}"
export DOCKER_DEFAULT_PLATFORM="linux/${arch}"
export VERIFY_DOCKER_PLATFORM="linux/${arch}"
export RUNSC_SOURCE=remote
export MC_SOURCE=remote
export AXERN_DOCKER_PUSH_AFTER_BUILD=1
export AXERN_DOCKER_GHA_CACHE="${AXERN_DOCKER_GHA_CACHE:-1}"
export AXERN_OCI_SOURCE_LABEL="https://github.com/cofy-x/axern"
bash "${AXERN_ROOT}/scripts/dev-env/build-images.sh"
