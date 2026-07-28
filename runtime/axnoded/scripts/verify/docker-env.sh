#!/usr/bin/env bash

default_verify_docker_platform() {
  local docker_platform
  if docker_platform="$(docker version --format '{{.Server.Os}}/{{.Server.Arch}}' 2>/dev/null)"; then
    case "${docker_platform}" in
      linux/amd64 | linux/arm64)
        printf '%s\n' "${docker_platform}"
        return 0
        ;;
    esac
  fi
  printf '%s\n' "linux/amd64"
}
