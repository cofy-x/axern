#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
mkdir -p "${dist}"
# Lint supplies a contract-only positive value; the packaged chart retains no
# production default and every deployment must provide its qualified reserve.
helm lint "${AXERN_ROOT}/deploy/helm/axern" \
  --set-string node.memorySystemReserveBytes=1
helm package "${AXERN_ROOT}/deploy/helm/axern" --destination "${dist}"
echo "helm_release_dist=${dist}"
