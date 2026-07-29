#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
arch="${1:-}"
case "${arch}" in
  amd64|arm64) ;;
  *) echo "usage: $0 <amd64|arm64> [build|push]" >&2; exit 2 ;;
esac
mode="${2:-push}"
case "${mode}" in
  build|push) ;;
  *) echo "usage: $0 <amd64|arm64> [build|push]" >&2; exit 2 ;;
esac

normalize_arch() {
  case "$1" in
    amd64|x86_64) printf 'amd64\n' ;;
    arm64|aarch64) printf 'arm64\n' ;;
    *) echo "unsupported architecture: $1" >&2; return 1 ;;
  esac
}

host_arch="$(normalize_arch "$(uname -m)")"
docker_arch="$(normalize_arch "$(docker info --format '{{.Architecture}}')")"
if [ "${host_arch}" != "${arch}" ] || [ "${docker_arch}" != "${arch}" ]; then
  echo "release image builds require a native ${arch} runner; host=${host_arch} docker=${docker_arch}" >&2
  exit 1
fi

base_tag="$(axern_release_version)"
export AXERN_RELEASE_VERSION="${base_tag}-${arch}"
axern_export_release_images
export AXERN_TARGET_GOARCH="${arch}"
export DOCKER_DEFAULT_PLATFORM="linux/${arch}"
export VERIFY_DOCKER_PLATFORM="linux/${arch}"
export RUNSC_SOURCE=remote
export MC_SOURCE=remote
if [ "${mode}" = "push" ]; then
  export AXERN_DOCKER_PUSH_AFTER_BUILD=1
else
  export AXERN_DOCKER_PUSH_AFTER_BUILD=0
fi
export AXERN_DOCKER_GHA_CACHE="${AXERN_DOCKER_GHA_CACHE:-1}"
export AXERN_OCI_SOURCE_LABEL="https://github.com/cofy-x/axern"

started_at="$(date +%s)"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    printf '### Release image build (%s)\n\n' "${arch}"
    printf '| Phase | Seconds |\n'
    printf '| --- | ---: |\n'
  } >> "${GITHUB_STEP_SUMMARY}"
fi
bash "${AXERN_ROOT}/scripts/dev-env/build-images.sh"
duration_seconds="$(( $(date +%s) - started_at ))"
printf 'release_image_arch=%s\n' "${arch}"
printf 'release_image_mode=%s\n' "${mode}"
printf 'release_image_duration_seconds=%s\n' "${duration_seconds}"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '| **Total** | **%s** |\n' "${duration_seconds}" >> "${GITHUB_STEP_SUMMARY}"
fi
