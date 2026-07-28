#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
syft_bin="${SYFT:-syft}"
candidate="$(mktemp -d)"
cleanup() { rm -rf "${candidate}"; }
trap cleanup EXIT

mkdir -p "${dist}"
(
  cd "${AXERN_ROOT}"
  while IFS= read -r -d '' path; do
    if [ -f "${path}" ] || [ -L "${path}" ]; then
      printf '%s\0' "${path}"
    fi
  done < <(git ls-files -z --cached --others --exclude-standard) |
    tar --null -T - -cf -
) | tar -xf - -C "${candidate}"

"${syft_bin}" scan "dir:${candidate}" \
  --source-name axern \
  --source-version "${version}" \
  -o "spdx-json=${dist}/axern-${version}.spdx.json" \
  -o "cyclonedx-json=${dist}/axern-${version}.cdx.json" >/dev/null
echo "release_sbom_dist=${dist}"
