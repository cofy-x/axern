#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
syft_bin="${SYFT:-syft}"
audit_root="$(mktemp -d)"
candidate="${audit_root}/tree"
cleanup() { rm -rf "${audit_root}"; }
trap cleanup EXIT

mkdir -p "${dist}" "${candidate}"
python3 "${AXERN_ROOT}/scripts/export-source-tree.py" "${candidate}"

"${syft_bin}" scan "dir:${candidate}" \
  --source-name axern \
  --source-version "${version}" \
  -o "spdx-json=${dist}/axern-${version}.spdx.json" \
  -o "cyclonedx-json=${dist}/axern-${version}.cdx.json" >/dev/null
echo "release_sbom_dist=${dist}"
