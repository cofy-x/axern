#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
output="${1:?usage: build-local-image-lock.sh OUTPUT}"
release_tag="$(axern_release_version)"
candidate_tag="candidate-${GITHUB_SHA:?GITHUB_SHA is required}"
mkdir -p "$(dirname "${output}")"
: >"${output}"

require_platforms() {
  local image="$1"
  local inspection
  inspection="$(docker buildx imagetools inspect "${image}")"
  grep -q 'linux/amd64' <<<"${inspection}" || { echo "${image} has no linux/amd64 manifest" >&2; return 1; }
  grep -q 'linux/arm64' <<<"${inspection}" || { echo "${image} has no linux/arm64 manifest" >&2; return 1; }
}

lock_image() {
  local key="$1"
  local image="$2"
  require_platforms "${image}"
  local digest
  digest="sha256:$(docker buildx imagetools inspect "${image}" --raw | sha256sum | awk '{print $1}')"
  printf '%s=%s@%s\n' "${key}" "${image%%:*}" "${digest}" >>"${output}"
}

declare -A internal=(
  [CONTROLD_IMAGE]=controld
  [TUNNELD_IMAGE]=tunneld
  [GATEWAYD_IMAGE]=gatewayd
  [NODE_ALL_IN_ONE_IMAGE]=node-all-in-one
  [PYTHON311_RUNTIME_IMAGE]=python311-runtime
  [SERVER_BASE_RUNTIME_IMAGE]=server-base-runtime
  [CODING_BASE_RUNTIME_IMAGE]=coding-base-runtime
  [DESKTOP_BASE_RUNTIME_IMAGE]=desktop-base-runtime
  [CLAUDE_CODE_BUNDLE_IMAGE]=claude-code-bundle
  [CODEX_BUNDLE_IMAGE]=codex-bundle
)
for key in "${!internal[@]}"; do
  name="${internal[${key}]}"
  target="${AXERN_RELEASE_REGISTRY}/${name}:${candidate_tag}"
  docker buildx imagetools create --tag "${target}" \
    "${AXERN_RELEASE_REGISTRY}/${name}:${release_tag}-amd64" \
    "${AXERN_RELEASE_REGISTRY}/${name}:${release_tag}-arm64"
  lock_image "${key}" "${target}"
done

lock_image POSTGRES_IMAGE postgres:16-alpine
lock_image MINIO_IMAGE minio/minio:RELEASE.2025-02-28T09-55-16Z
lock_image OTEL_COLLECTOR_IMAGE otel/opentelemetry-collector:0.150.1
lock_image OTEL_LGTM_IMAGE grafana/otel-lgtm:0.11.16
LC_ALL=C sort -o "${output}" "${output}"
echo "local_image_lock=${output}"
