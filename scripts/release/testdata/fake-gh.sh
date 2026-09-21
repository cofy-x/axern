#!/usr/bin/env bash
set -euo pipefail

if [ "$1 $2" = "release view" ]; then
  if [ "${FAKE_GH_VIEW_ERROR:-}" = "1" ]; then
    echo "transport error while querying release" >&2
    exit 1
  fi
  if [ ! -d "${FAKE_GH_STATE}/assets" ]; then
    echo "release not found" >&2
    exit 1
  fi
  [ -d "${FAKE_GH_STATE}/assets" ]
  exit
fi
if [ "$1 $2" = "release download" ]; then
  destination=""
  shift 3
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --dir) destination="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  cp "${FAKE_GH_STATE}/assets/"* "${destination}/"
  exit
fi
if [ "$1 $2" = "release create" ]; then
  mkdir -p "${FAKE_GH_STATE}/assets"
  cp "${FAKE_GH_DIST}/"* "${FAKE_GH_STATE}/assets/"
  count=0
  if [ -f "${FAKE_GH_STATE}/create-count" ]; then
    count="$(cat "${FAKE_GH_STATE}/create-count")"
  fi
  printf '%s\n' "$((count + 1))" > "${FAKE_GH_STATE}/create-count"
  exit
fi
echo "unsupported fake gh command: $*" >&2
exit 2
