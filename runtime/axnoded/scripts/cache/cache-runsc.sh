#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${ROOT_DIR}/runtime-tools.sh"

RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH:-aarch64}"
RUNSC_CACHE_REFRESH="${RUNSC_CACHE_REFRESH:-false}"
RUNSC_CACHE_ROOT="${RUNSC_CACHE_ROOT:-${ROOT_DIR}/.cache/gvisor}"
RUNSC_RELEASE="${RUNSC_RELEASE:-${AXERN_GVISOR_RELEASE}}"
RUNSC_RELEASE_BASE_URL="${RUNSC_RELEASE_BASE_URL:-https://storage.googleapis.com/gvisor/releases/release}"

case "${RUNSC_CACHE_ARCH}" in
  x86_64|amd64)
    RUNSC_RELEASE_ARCH=x86_64
    RUNSC_ARCHIVE_SHA512="${AXERN_GVISOR_SHA512_AMD64}"
    ;;
  aarch64|arm64)
    RUNSC_RELEASE_ARCH=aarch64
    RUNSC_ARCHIVE_SHA512="${AXERN_GVISOR_SHA512_ARM64}"
    ;;
  *)
    echo "unsupported runsc cache architecture: ${RUNSC_CACHE_ARCH}" >&2
    exit 2
    ;;
esac
cache_dir="${RUNSC_CACHE_ROOT}/${RUNSC_CACHE_ARCH}"
runsc_path="${cache_dir}/runsc"
release_path="${cache_dir}/.release"
url="${RUNSC_RELEASE_BASE_URL}/${RUNSC_RELEASE}/${RUNSC_RELEASE_ARCH}/gvisor.tar.bz2"

mkdir -p "${cache_dir}"

verify_sha512() {
  local archive="$1"
  if command -v sha512sum >/dev/null 2>&1; then
    printf '%s  %s\n' "${RUNSC_ARCHIVE_SHA512}" "${archive}" | sha512sum -c -
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf '%s  %s\n' "${RUNSC_ARCHIVE_SHA512}" "${archive}" | shasum -a 512 -c -
    return 0
  fi
  echo "sha512sum or shasum is required to verify cached runsc" >&2
  return 1
}

if [ "${RUNSC_CACHE_REFRESH}" != "true" ] && \
   [ -x "${runsc_path}" ] && \
   [ -x "${cache_dir}/gvisor-bin/checkpointgofer" ] && \
   [ "$(cat "${release_path}" 2>/dev/null || true)" = "${RUNSC_RELEASE}" ]; then
  echo "runsc_cache_ready=true"
  echo "runsc_cache_path=${runsc_path}"
  exit 0
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/runsc-cache.XXXXXX")"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 \
  -fsSLo "${tmpdir}/gvisor.tar.bz2" "${url}"
verify_sha512 "${tmpdir}/gvisor.tar.bz2" >/dev/null
mkdir -p "${tmpdir}/extracted"
tar -xjf "${tmpdir}/gvisor.tar.bz2" -C "${tmpdir}/extracted"

rm -rf "${cache_dir}"
mkdir -p "${cache_dir}"
cp -a "${tmpdir}/extracted/." "${cache_dir}/"
printf '%s\n' "${RUNSC_RELEASE}" > "${release_path}"
chmod 0755 "${runsc_path}" "${cache_dir}/containerd-shim-runsc-v1" "${cache_dir}"/gvisor-bin/*

echo "runsc_cache_ready=true"
echo "runsc_cache_path=${runsc_path}"
