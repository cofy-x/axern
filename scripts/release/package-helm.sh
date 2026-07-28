#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
mkdir -p "${dist}"
helm lint "${AXERN_ROOT}/deploy/helm/axern"
helm package "${AXERN_ROOT}/deploy/helm/axern" --destination "${dist}"
echo "helm_release_dist=${dist}"
