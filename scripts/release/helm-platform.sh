#!/usr/bin/env bash
set -euo pipefail

system="${1:?usage: $0 <system> <machine>}"
machine="${2:?usage: $0 <system> <machine>}"
version=v3.18.6

case "${system}-${machine}" in
  Linux-x86_64|Linux-amd64)
    printf '%s %s %s\n' \
      "${version}" \
      linux-amd64 \
      3f43c0aa57243852dd542493a0f54f1396c0bc8ec7296bbb2c01e802010819ce
    ;;
  Linux-aarch64|Linux-arm64)
    printf '%s %s %s\n' \
      "${version}" \
      linux-arm64 \
      5b8e00b6709caab466cbbb0bc29ee09059b8dc9417991dd04b497530e49b1737
    ;;
  *)
    echo "the release workflow Helm installer supports Linux amd64 and arm64; got ${system}-${machine}" >&2
    exit 1
    ;;
esac
