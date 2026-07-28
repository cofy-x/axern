#!/usr/bin/env bash

axern_docker_cache_ref() {
  local image_ref="$1"
  if [ -z "${image_ref}" ] || [ "${image_ref}" = "${image_ref%:*}" ]; then
    return 1
  fi
  printf '%s:buildcache\n' "${image_ref%:*}"
}

axern_docker_cache_scope() {
  local image_ref="$1"
  local image_name="${image_ref##*/}"
  if [ "${image_name}" != "${image_name%:*}" ]; then
    image_ref="${image_ref%:*}"
  fi
  image_ref="${image_ref#*/}"
  image_ref="${image_ref//[^A-Za-z0-9_.-]/-}"
  printf '%s\n' "${image_ref}"
}

axern_docker_build() {
  local image_ref=""
  local args=()
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -t|--tag)
        if [ "$#" -lt 2 ]; then
          echo "missing value for $1" >&2
          return 1
        fi
        image_ref="$2"
        args+=("$1" "$2")
        shift 2
        ;;
      *)
        args+=("$1")
        shift
        ;;
    esac
  done

  # GitHub Actions release jobs use the gha backend because some registries do not
  # accept BuildKit cache manifests.
  if [ "${AXERN_DOCKER_GHA_CACHE:-}" = "1" ] && [ -n "${image_ref}" ] && docker buildx version >/dev/null 2>&1; then
    local cache_scope=""
    cache_scope="$(axern_docker_cache_scope "${image_ref}")"
    docker buildx build \
      --load \
      --cache-from "type=gha,scope=${cache_scope}" \
      --cache-to "type=gha,scope=${cache_scope},mode=max" \
      "${args[@]}"
    return $?
  fi

  # Keep registry cache as an opt-in path for registries that support BuildKit
  # cache manifests.
  if [ "${AXERN_DOCKER_REGISTRY_CACHE:-}" = "1" ] && [ -n "${image_ref}" ] && docker buildx version >/dev/null 2>&1; then
    local cache_ref=""
    if cache_ref="$(axern_docker_cache_ref "${image_ref}")"; then
      docker buildx build \
        --load \
        --cache-from "type=registry,ref=${cache_ref}" \
        --cache-to "type=registry,ref=${cache_ref},mode=max" \
        "${args[@]}"
      return $?
    fi
  fi

  DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" docker build "${args[@]}"
}
