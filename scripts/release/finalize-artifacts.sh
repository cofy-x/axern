#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_RELEASE_DIST:-${AXERN_ROOT}/dist/release}"
checksums_tmp="$(mktemp)"
cleanup() { rm -f "${checksums_tmp}"; }
trap cleanup EXIT
(
  cd "${dist}"
  find . -maxdepth 1 -type f ! -name checksums.txt -print | sed 's#^./##' | LC_ALL=C sort | xargs shasum -a 256 > "${checksums_tmp}"
)
chmod 0644 "${checksums_tmp}"
mv "${checksums_tmp}" "${dist}/checksums.txt"
echo "release_checksums=${dist}/checksums.txt"
