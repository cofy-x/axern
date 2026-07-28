#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

MC_CACHE_ARCH="${MC_CACHE_ARCH:-arm64}"
MC_CACHE_REFRESH="${MC_CACHE_REFRESH:-false}"
MC_CACHE_ROOT="${MC_CACHE_ROOT:-${ROOT_DIR}/.cache/minio}"
MC_RELEASE_BASE_URL="${MC_RELEASE_BASE_URL:-https://dl.min.io/client/mc/release}"

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
cache_dir="${MC_CACHE_ROOT}/${mc_arch}"
mc_path="${cache_dir}/mc"
url="${MC_RELEASE_BASE_URL}/linux-${mc_arch}/mc"

mkdir -p "${cache_dir}"

if [ "${MC_CACHE_REFRESH}" != "true" ] && [ -f "${mc_path}" ] && [ -x "${mc_path}" ]; then
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
mv "${tmpdir}/mc" "${mc_path}"
chmod 0755 "${mc_path}"

echo "mc_cache_ready=true"
echo "mc_cache_path=${mc_path}"
