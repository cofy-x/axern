#!/usr/bin/env bash
set -euo pipefail

go_bin="${GO:-go}"
env_args=(
  "GOTOOLCHAIN=local"
  "GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}"
  "PATH=${PATH}"
)

for name in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
  value="${!name:-}"
  if [ -n "${value}" ]; then
    env_args+=("${name}=${value}")
  fi
done

exec sudo -n env "${env_args[@]}" "${go_bin}" "$@"
