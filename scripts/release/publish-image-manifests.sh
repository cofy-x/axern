#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
tag="$(axern_release_version)"
lock_file="${AXERN_LOCAL_IMAGE_LOCK_FILE:?AXERN_LOCAL_IMAGE_LOCK_FILE is required}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

inspect_digest() {
  local target="$1"
  local output="$2"
  local error_output="$3"
  if docker buildx imagetools inspect "${target}" --raw >"${output}" 2>"${error_output}"; then
    printf 'sha256:%s\n' "$(sha256sum "${output}" | awk '{print $1}')"
    return 0
  fi
  if grep -Eiq 'manifest unknown|not found' "${error_output}"; then
    return 1
  fi
  cat "${error_output}" >&2
  return 2
}

lock_key() {
  case "$1" in
    controld) printf '%s\n' CONTROLD_IMAGE ;;
    tunneld) printf '%s\n' TUNNELD_IMAGE ;;
    gatewayd) printf '%s\n' GATEWAYD_IMAGE ;;
    node-all-in-one) printf '%s\n' NODE_ALL_IN_ONE_IMAGE ;;
    *) echo "unsupported release image $1" >&2; return 1 ;;
  esac
}

for image in controld tunneld gatewayd node-all-in-one; do
  target="${AXERN_RELEASE_REGISTRY}/${image}:${tag}"
  key="$(lock_key "${image}")"
  locked_ref="$(awk -F '=' -v key="${key}" '$1 == key { print $2 }' "${lock_file}")"
  if [ -z "${locked_ref}" ] || [[ "${locked_ref}" != *@sha256:* ]]; then
    echo "release image lock is missing an immutable ${image} reference" >&2
    exit 1
  fi
  expected="${locked_ref##*@}"
  output="${tmp_dir}/${image}.json"
  error_output="${tmp_dir}/${image}.err"
  if actual="$(inspect_digest "${target}" "${output}" "${error_output}")"; then
    if [ "${actual}" != "${expected}" ]; then
      echo "published manifest ${target} digest ${actual} does not match locked digest ${expected}" >&2
      exit 1
    fi
    echo "release_image_manifest_already_published=${target}@${actual}"
  else
    status=$?
    if [ "${status}" -ne 1 ]; then
      exit "${status}"
    fi
    docker buildx imagetools create --tag "${target}" "${locked_ref}"
  fi
  docker buildx imagetools inspect "${target}"
  actual="$(inspect_digest "${target}" "${output}" "${error_output}")"
  [ "${actual}" = "${expected}" ] || {
    echo "published manifest ${target} digest ${actual} does not match locked digest ${expected}" >&2
    exit 1
  }
done
