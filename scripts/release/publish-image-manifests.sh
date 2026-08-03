#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
tag="$(axern_release_version)"
declare -A lock_keys=(
  [controld]=CONTROLD_IMAGE
  [tunneld]=TUNNELD_IMAGE
  [gatewayd]=GATEWAYD_IMAGE
  [node-all-in-one]=NODE_ALL_IN_ONE_IMAGE
  [python311-runtime]=PYTHON311_RUNTIME_IMAGE
  [server-base-runtime]=SERVER_BASE_RUNTIME_IMAGE
  [coding-base-runtime]=CODING_BASE_RUNTIME_IMAGE
  [desktop-base-runtime]=DESKTOP_BASE_RUNTIME_IMAGE
  [claude-code-bundle]=CLAUDE_CODE_BUNDLE_IMAGE
  [codex-bundle]=CODEX_BUNDLE_IMAGE
)
for image in "${!lock_keys[@]}"; do
  target="${AXERN_RELEASE_REGISTRY}/${image}:${tag}"
  locked_ref=""
  if [ -n "${AXERN_LOCAL_IMAGE_LOCK_FILE:-}" ]; then
    locked_ref="$(awk -F '=' -v key="${lock_keys[${image}]}" '$1 == key { print $2 }' "${AXERN_LOCAL_IMAGE_LOCK_FILE}")"
  fi
  if [ -n "${locked_ref}" ]; then
    docker buildx imagetools create --tag "${target}" "${locked_ref}"
  else
    docker buildx imagetools create --tag "${target}" "${target}-amd64" "${target}-arm64"
  fi
  docker buildx imagetools inspect "${target}"
  if [ -n "${AXERN_LOCAL_IMAGE_LOCK_FILE:-}" ]; then
    actual="sha256:$(docker buildx imagetools inspect "${target}" --raw | sha256sum | awk '{print $1}')"
    expected="${locked_ref##*@}"
    [ "${actual}" = "${expected}" ] || {
      echo "published manifest ${target} digest ${actual} does not match locked digest ${expected}" >&2
      exit 1
    }
  fi
done
