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
}

axern_release_images() {
  printf '%s\n' \
    "${CONTROLD_IMAGE}" \
    "${TUNNELD_IMAGE}" \
    "${GATEWAYD_IMAGE}" \
    "${NODE_ALL_IN_ONE_IMAGE}"
}
