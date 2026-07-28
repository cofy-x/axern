#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH:-aarch64}"
RUNSC_CACHE_REFRESH="${RUNSC_CACHE_REFRESH:-false}"
RUNSC_CACHE_ROOT="${RUNSC_CACHE_ROOT:-${ROOT_DIR}/.cache/gvisor}"
RUNSC_RELEASE_CHANNEL="${RUNSC_RELEASE_CHANNEL:-latest}"
RUNSC_RELEASE_BASE_URL="${RUNSC_RELEASE_BASE_URL:-https://storage.googleapis.com/gvisor/releases/release}"

cache_dir="${RUNSC_CACHE_ROOT}/${RUNSC_CACHE_ARCH}"
runsc_path="${cache_dir}/runsc"
sha_path="${cache_dir}/runsc.sha512"
url="${RUNSC_RELEASE_BASE_URL}/${RUNSC_RELEASE_CHANNEL}/${RUNSC_CACHE_ARCH}/runsc"
sha_url="${RUNSC_RELEASE_BASE_URL}/${RUNSC_RELEASE_CHANNEL}/${RUNSC_CACHE_ARCH}/runsc.sha512"

mkdir -p "${cache_dir}"

verify_sha512() {
  if command -v sha512sum >/dev/null 2>&1; then
    (cd "${cache_dir}" && sha512sum -c "$(basename "${sha_path}")")
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    (cd "${cache_dir}" && shasum -a 512 -c "$(basename "${sha_path}")")
    return 0
  fi
  echo "sha512sum or shasum is required to verify cached runsc" >&2
  return 1
}

if [ "${RUNSC_CACHE_REFRESH}" != "true" ] && [ -f "${runsc_path}" ] && [ -f "${sha_path}" ]; then
  if verify_sha512 >/dev/null 2>&1; then
    echo "runsc_cache_ready=true"
    echo "runsc_cache_path=${runsc_path}"
    exit 0
  fi
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/runsc-cache.XXXXXX")"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 \
  -fsSLo "${tmpdir}/runsc" "${url}"
curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 \
  -fsSLo "${tmpdir}/runsc.sha512" "${sha_url}"

mv "${tmpdir}/runsc" "${runsc_path}"
mv "${tmpdir}/runsc.sha512" "${sha_path}"
chmod 0755 "${runsc_path}"
verify_sha512 >/dev/null

echo "runsc_cache_ready=true"
echo "runsc_cache_path=${runsc_path}"
