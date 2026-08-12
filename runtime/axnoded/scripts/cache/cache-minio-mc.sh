#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
AXERN_GVISOR_LOCK="${ROOT_DIR}/gvisor.lock"
. "${ROOT_DIR}/runtime-tools.sh"

MC_CACHE_ARCH="${MC_CACHE_ARCH:-arm64}"
MC_CACHE_REFRESH="${MC_CACHE_REFRESH:-false}"
MC_CACHE_ROOT="${MC_CACHE_ROOT:-${ROOT_DIR}/.cache/minio}"
MC_RELEASE_BASE_URL="${MC_RELEASE_BASE_URL:-https://dl.min.io/client/mc/release}"
MC_RELEASE="${MC_RELEASE:-${AXERN_MC_RELEASE}}"

normalize_mc_arch() {
  case "$1" in
    arm64|aarch64) echo "arm64" ;;
    amd64|x86_64) echo "amd64" ;;
    *)
      echo "unsupported mc arch: $1" >&2
      return 1
      ;;
  esac
}

mc_arch="$(normalize_mc_arch "${MC_CACHE_ARCH}")"
case "${mc_arch}" in
  amd64) MC_SHA256="${AXERN_MC_SHA256_AMD64}" ;;
  arm64) MC_SHA256="${AXERN_MC_SHA256_ARM64}" ;;
esac
cache_dir="${MC_CACHE_ROOT}/${mc_arch}"
mc_path="${cache_dir}/mc"
url="${MC_RELEASE_BASE_URL}/linux-${mc_arch}/archive/mc.${MC_RELEASE}"

mkdir -p "${cache_dir}"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

actual_sha256=""
if [ -f "${mc_path}" ]; then
  actual_sha256="$(sha256_file "${mc_path}")"
fi
if [ "${MC_CACHE_REFRESH}" != "true" ] && [ -x "${mc_path}" ] && [ "${actual_sha256}" = "${MC_SHA256}" ]; then
  echo "mc_cache_ready=true"
  echo "mc_cache_path=${mc_path}"
  exit 0
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/mc-cache.XXXXXX")"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

curl --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 10 --max-time 900 \
  -fsSLo "${tmpdir}/mc" "${url}"
[ "$(sha256_file "${tmpdir}/mc")" = "${MC_SHA256}" ] || {
  echo "downloaded mc checksum does not match the pinned release" >&2
  exit 1
}
mv "${tmpdir}/mc" "${mc_path}"
chmod 0755 "${mc_path}"

echo "mc_cache_ready=true"
echo "mc_cache_path=${mc_path}"
