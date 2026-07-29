#!/usr/bin/env bash

axern_docker_cache_scope() {
  local image_ref="$1"
  local image_name="${image_ref##*/}"
  if [ "${image_name}" != "${image_name%:*}" ]; then
    image_ref="${image_ref%:*}"
  fi
  image_ref="${image_ref#*/}"
  image_ref="${image_ref//[^A-Za-z0-9_.-]/-}"
  printf '%s%s\n' "${image_ref}" "${AXERN_TARGET_GOARCH:+-${AXERN_TARGET_GOARCH}}"
}

axern_require_gha_cache_runtime() {
  if ! docker buildx version >/dev/null 2>&1; then
    echo "Docker Buildx is required when AXERN_DOCKER_CACHE_BACKEND=gha" >&2
    return 1
  fi
  if [ -z "${ACTIONS_RUNTIME_TOKEN:-}" ]; then
    echo "ACTIONS_RUNTIME_TOKEN is required when AXERN_DOCKER_CACHE_BACKEND=gha" >&2
    return 1
  fi
  if [ -z "${ACTIONS_RESULTS_URL:-}" ] && [ -z "${ACTIONS_CACHE_URL:-}" ]; then
    echo "ACTIONS_RESULTS_URL or ACTIONS_CACHE_URL is required when AXERN_DOCKER_CACHE_BACKEND=gha" >&2
    return 1
  fi
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

  if [ -n "${AXERN_OCI_SOURCE_LABEL:-}" ]; then
    args+=(--label "org.opencontainers.image.source=${AXERN_OCI_SOURCE_LABEL}")
  fi

  case "${AXERN_DOCKER_CACHE_BACKEND:-none}" in
    none)
      ;;
    gha)
      if [ -z "${image_ref}" ]; then
        echo "a tagged image is required when AXERN_DOCKER_CACHE_BACKEND=gha" >&2
        return 1
      fi
      axern_require_gha_cache_runtime || return 1
      local cache_scope=""
      cache_scope="$(axern_docker_cache_scope "${image_ref}")"
      printf 'docker_build_cache_backend=gha scope=%s\n' "${cache_scope}"
      docker buildx build \
        --load \
        --cache-from "type=gha,scope=${cache_scope},timeout=20m" \
        --cache-to "type=gha,scope=${cache_scope},mode=max,timeout=20m" \
        "${args[@]}"
      return $?
      ;;
    *)
      echo "AXERN_DOCKER_CACHE_BACKEND must be none or gha" >&2
      return 1
      ;;
  esac

  DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" docker build "${args[@]}"
}
