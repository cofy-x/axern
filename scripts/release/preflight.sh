#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
tag="$(axern_release_version)"
version="${tag#v}"

if gh release view "${tag}" >/dev/null 2>&1; then
  echo "GitHub release ${tag} already exists" >&2
  exit 1
fi
for image in controld tunneld gatewayd node-all-in-one python311-runtime server-base-runtime coding-base-runtime desktop-base-runtime claude-code-bundle codex-bundle; do
  if docker buildx imagetools inspect "${AXERN_RELEASE_REGISTRY}/${image}:${tag}" >/dev/null 2>&1; then
    echo "final image tag already exists: ${AXERN_RELEASE_REGISTRY}/${image}:${tag}" >&2
    exit 1
  fi
done
if helm show chart "oci://ghcr.io/cofy-x/charts/axern" --version "${version}" >/dev/null 2>&1; then
  echo "Helm chart version already exists: ${version}" >&2
  exit 1
fi
echo "release_preflight_ok=${tag}"
