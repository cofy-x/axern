#!/usr/bin/env bash
set -euo pipefail

if [ "$1 $2 $3" != "buildx imagetools inspect" ] && [ "$1 $2 $3" != "buildx imagetools create" ]; then
  echo "unsupported fake Docker command: $*" >&2
  exit 2
fi
operation="$3"
shift 3
case "${operation}" in
  inspect)
    target="$1"
    state="${FAKE_DOCKER_STATE}/${target##*/}"
    if [ ! -f "${state}" ]; then
      echo "manifest unknown" >&2
      exit 1
    fi
    if [ "${2:-}" = "--raw" ]; then
      cat "${state}"
    else
      echo "Name: ${target}"
    fi
    ;;
  create)
    if [ "$1" != "--tag" ]; then
      echo "fake Docker create requires --tag" >&2
      exit 2
    fi
    target="$2"
    cp "${FAKE_DOCKER_RAW}" "${FAKE_DOCKER_STATE}/${target##*/}"
    count=0
    if [ -f "${FAKE_DOCKER_STATE}/create-count" ]; then
      count="$(cat "${FAKE_DOCKER_STATE}/create-count")"
    fi
    printf '%s\n' "$((count + 1))" > "${FAKE_DOCKER_STATE}/create-count"
    ;;
esac
