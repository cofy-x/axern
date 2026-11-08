#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
AXERN_GVISOR_LOCK="${ROOT_DIR}/gvisor.lock"
. "${ROOT_DIR}/runtime-tools.sh"

RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH:-aarch64}"
RUNSC_CACHE_REFRESH="${RUNSC_CACHE_REFRESH:-false}"
RUNSC_CACHE_ROOT="${RUNSC_CACHE_ROOT:-${ROOT_DIR}/.cache/gvisor}"
RUNSC_RELEASE_BASE_URL="${RUNSC_RELEASE_BASE_URL:-https://storage.googleapis.com/gvisor/releases/release}"
case "${RUNSC_CACHE_ARCH}" in
  x86_64|amd64) RUNSC_RELEASE_ARCH=x86_64; target_arch=amd64 ;;
  aarch64|arm64) RUNSC_RELEASE_ARCH=aarch64; target_arch=arm64 ;;
  *) echo "unsupported runsc cache architecture: ${RUNSC_CACHE_ARCH}" >&2; exit 2 ;;
esac
cache_dir="${RUNSC_CACHE_ROOT}/${RUNSC_CACHE_ARCH}"
url="${RUNSC_RELEASE_BASE_URL}/${AXERN_GVISOR_RELEASE}/${RUNSC_RELEASE_ARCH}/gvisor.tar.bz2"
selector=(python3 "${ROOT_DIR}/gvisor_payload.py")
selection=(--arch "${target_arch}" --lock "${ROOT_DIR}/gvisor-selected.lock.json" --upstream-lock "${AXERN_GVISOR_LOCK}")

if [ "${RUNSC_CACHE_REFRESH}" != true ] && { [ -e "${cache_dir}" ] || [ -L "${cache_dir}" ]; }; then
  "${selector[@]}" verify "${selection[@]}" --source "${cache_dir}" || {
    echo "cache differs from selected profile; choose a new cache root or explicitly refresh" >&2; exit 1;
  }
else
  mkdir -p "${RUNSC_CACHE_ROOT}"
  tmpdir="$(mktemp -d "${RUNSC_CACHE_ROOT}/.runsc-cache.XXXXXX")"
  trap 'rm -rf "${tmpdir}"' EXIT
  curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 \
    -fsSLo "${tmpdir}/gvisor.tar.bz2" "${url}"
  "${selector[@]}" extract "${selection[@]}" --source "${tmpdir}/gvisor.tar.bz2" --destination "${tmpdir}/selected"
  if [ -e "${cache_dir}" ] || [ -L "${cache_dir}" ]; then
    # Explicit refresh preserves the previous cache for inspection.
    previous="$(mktemp -d "${RUNSC_CACHE_ROOT}/.previous-${RUNSC_CACHE_ARCH}.XXXXXX")"
    mv "${cache_dir}" "${previous}/payload"
    echo "runsc_cache_previous=${previous}/payload"
  fi
  python3 -c 'import os, sys; os.rename(sys.argv[1], sys.argv[2])' "${tmpdir}/selected" "${cache_dir}"
fi

echo "runsc_cache_ready=true"
echo "runsc_cache_path=${cache_dir}/runsc"
