#!/usr/bin/env bash

AXERN_RELEASE_REGISTRY="${AXERN_RELEASE_REGISTRY:-ghcr.io/cofy-x/axern}"

axern_release_version() {
  local version="${AXERN_RELEASE_VERSION:-}"
  if [ -z "${version}" ]; then
    version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
  fi
  case "${version}" in
    v*) printf '%s\n' "${version}" ;;
    *) printf 'v%s\n' "${version}" ;;
  esac
}

axern_export_release_images() {
  local tag
  tag="$(axern_release_version)"
  export NODE_RUNTIME_BASE_IMAGE_TAG="${AXERN_RELEASE_REGISTRY}/node-runtime-base:${tag}"
  export CONTROLD_IMAGE="${AXERN_RELEASE_REGISTRY}/controld:${tag}"
  export TUNNELD_IMAGE="${AXERN_RELEASE_REGISTRY}/tunneld:${tag}"
  export GATEWAYD_IMAGE="${AXERN_RELEASE_REGISTRY}/gatewayd:${tag}"
  export NODE_ALL_IN_ONE_IMAGE="${AXERN_RELEASE_REGISTRY}/node-all-in-one:${tag}"
  export PYTHON311_RUNTIME_IMAGE="${AXERN_RELEASE_REGISTRY}/python311-runtime:${tag}"
  export SERVER_BASE_RUNTIME_IMAGE="${AXERN_RELEASE_REGISTRY}/server-base-runtime:${tag}"
  export CODING_BASE_RUNTIME_IMAGE="${AXERN_RELEASE_REGISTRY}/coding-base-runtime:${tag}"
  export DESKTOP_BASE_RUNTIME_IMAGE="${AXERN_RELEASE_REGISTRY}/desktop-base-runtime:${tag}"
  export CLAUDE_CODE_BUNDLE_IMAGE="${AXERN_RELEASE_REGISTRY}/claude-code-bundle:${tag}"
  export CODEX_BUNDLE_IMAGE="${AXERN_RELEASE_REGISTRY}/codex-bundle:${tag}"
}

axern_release_images() {
  printf '%s\n' \
    "${CONTROLD_IMAGE}" \
    "${TUNNELD_IMAGE}" \
    "${GATEWAYD_IMAGE}" \
    "${NODE_ALL_IN_ONE_IMAGE}" \
    "${PYTHON311_RUNTIME_IMAGE}" \
    "${SERVER_BASE_RUNTIME_IMAGE}" \
    "${CODING_BASE_RUNTIME_IMAGE}" \
    "${DESKTOP_BASE_RUNTIME_IMAGE}" \
    "${CLAUDE_CODE_BUNDLE_IMAGE}" \
    "${CODEX_BUNDLE_IMAGE}"
}
