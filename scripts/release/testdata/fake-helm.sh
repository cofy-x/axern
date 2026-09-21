#!/usr/bin/env bash
set -euo pipefail

command_name="${1:?command is required}"
shift
case "${command_name}" in
  pull)
    destination=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --destination) destination="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    if [ ! -f "${FAKE_HELM_STATE}/remote.tgz" ]; then
      echo "manifest unknown: not found" >&2
      exit 1
    fi
    cp "${FAKE_HELM_STATE}/remote.tgz" "${destination}/axern-${FAKE_HELM_VERSION}.tgz"
    ;;
  push)
    chart="${1:?chart is required}"
    count=0
    if [ -f "${FAKE_HELM_STATE}/push-count" ]; then
      count="$(cat "${FAKE_HELM_STATE}/push-count")"
    fi
    count="$((count + 1))"
    printf '%s\n' "${count}" > "${FAKE_HELM_STATE}/push-count"
    case "${FAKE_HELM_MODE}" in
      transient)
        if [ "${count}" -eq 1 ]; then
          echo 'Error: failed to perform "Tag" on destination: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: not found' >&2
          exit 1
        fi
        ;;
      uncertain)
        cp "${chart}" "${FAKE_HELM_STATE}/remote.tgz"
        echo 'Error: failed to perform "Tag" on destination: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: not found' >&2
        exit 1
        ;;
      permanent)
        echo "Error: unauthorized" >&2
        exit 1
        ;;
      success) ;;
      *) echo "unsupported fake Helm mode ${FAKE_HELM_MODE}" >&2; exit 2 ;;
    esac
    cp "${chart}" "${FAKE_HELM_STATE}/remote.tgz"
    echo "Pushed: fake/axern:${FAKE_HELM_VERSION}"
    ;;
  *)
    echo "unsupported fake Helm command ${command_name}" >&2
    exit 2
    ;;
esac
