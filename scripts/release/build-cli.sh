#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
go_bin="${GO:-go}"
image_lock=""
if [ -n "${AXERN_LOCAL_IMAGE_LOCK_FILE:-}" ]; then
  for key in POSTGRES_IMAGE MINIO_IMAGE CONTROLD_IMAGE TUNNELD_IMAGE GATEWAYD_IMAGE NODE_ALL_IN_ONE_IMAGE PYTHON311_RUNTIME_IMAGE SERVER_BASE_RUNTIME_IMAGE CODING_BASE_RUNTIME_IMAGE DESKTOP_BASE_RUNTIME_IMAGE CLAUDE_CODE_BUNDLE_IMAGE CODEX_BUNDLE_IMAGE OTEL_COLLECTOR_IMAGE OTEL_LGTM_IMAGE; do
    grep -Eq "^${key}=[^[:space:]]+@sha256:[0-9a-f]{64}$" "${AXERN_LOCAL_IMAGE_LOCK_FILE}" || {
      echo "local image lock is missing a valid ${key}" >&2
      exit 1
    }
  done
  image_lock="$(awk 'NF { if (value != "") value = value ";"; value = value $0 } END { print value }' "${AXERN_LOCAL_IMAGE_LOCK_FILE}")"
  [ -n "${image_lock}" ] || { echo "local image lock is empty" >&2; exit 1; }
fi
rm -rf "${dist}"
mkdir -p "${dist}"
work_root="$(mktemp -d)"
cleanup() { rm -rf "${work_root}"; }
trap cleanup EXIT

for os in darwin linux; do
  for arch in amd64 arm64; do
    work="${work_root}/${os}-${arch}"
    mkdir -p "${work}"
    archive="axern_${version}_${os}_${arch}.tar.gz"
    (
      cd "${AXERN_ROOT}"
      ldflags="-s -w -X github.com/cofy-x/axern/sdk/go.version=${version}"
      if [ -n "${image_lock}" ]; then
        ldflags="${ldflags} -X github.com/cofy-x/axern/apps/cli/internal/localbundle.imageLock=${image_lock}"
      fi
      CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" "${go_bin}" build \
        -trimpath -ldflags "${ldflags}" \
        -o "${work}/axern" ./apps/cli
    )
    cp "${AXERN_ROOT}/LICENSE" "${work}/LICENSE"
    tar -C "${work}" -czf "${dist}/${archive}" axern LICENSE
  done
done
cp "${AXERN_ROOT}/install.sh" "${dist}/install.sh"
chmod 0755 "${dist}/install.sh"
if [ -n "${AXERN_LOCAL_IMAGE_LOCK_FILE:-}" ]; then
  cp "${AXERN_LOCAL_IMAGE_LOCK_FILE}" "${dist}/images.lock"
  chmod 0644 "${dist}/images.lock"
fi
echo "cli_release_dist=${dist}"
