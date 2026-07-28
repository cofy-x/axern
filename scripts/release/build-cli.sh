#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
go_bin="${GO:-go}"
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
      CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" "${go_bin}" build \
        -trimpath -ldflags "-s -w -X github.com/cofy-x/axern/sdk/go.version=${version}" \
        -o "${work}/axern" ./apps/cli
    )
    cp "${AXERN_ROOT}/LICENSE" "${work}/LICENSE"
    tar -C "${work}" -czf "${dist}/${archive}" axern LICENSE
  done
done
echo "cli_release_dist=${dist}"
