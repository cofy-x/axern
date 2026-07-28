#!/usr/bin/env bash

set -euo pipefail

ROOTDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GITLEAKS_BIN="${GITLEAKS:-gitleaks}"
SYFT_BIN="${SYFT:-syft}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required open-source audit tool: $1" >&2
    exit 1
  fi
}

require_command git
require_command tar
require_command python3
require_command "${GITLEAKS_BIN}"
require_command "${SYFT_BIN}"

audit_root="$(mktemp -d "${TMPDIR:-/tmp}/axern-open-source-check.XXXXXX")"
trap 'rm -rf -- "${audit_root}"' EXIT
candidate="${audit_root}/tree"
mkdir -p "${candidate}"

cd "${ROOTDIR}"
while IFS= read -r -d '' path; do
  if [[ -f "${path}" || -L "${path}" ]]; then
    printf '%s\0' "${path}"
  fi
done < <(git ls-files -z --cached --others --exclude-standard) |
  tar --null -T - -cf - |
  tar -xf - -C "${candidate}"

surface_args=("${candidate}")
if [[ -n "${AXERN_OPEN_SOURCE_DENYLIST:-}" ]]; then
  if [[ ! -f "${AXERN_OPEN_SOURCE_DENYLIST}" ]]; then
    echo "open-source denylist does not exist: ${AXERN_OPEN_SOURCE_DENYLIST}" >&2
    exit 1
  fi
  surface_args+=("${AXERN_OPEN_SOURCE_DENYLIST}")
fi
python3 "${ROOTDIR}/scripts/open-source-public-surface.py" "${surface_args[@]}"

"${GITLEAKS_BIN}" dir "${candidate}" \
  --no-banner \
  --redact \
  --report-format json \
  --report-path "${audit_root}/gitleaks.json"
echo "open_source_secret_scan=passed"

"${SYFT_BIN}" scan "dir:${candidate}" \
  --source-name axern \
  --source-version 0.1.0 \
  -o "spdx-json=${audit_root}/sbom.spdx.json" >/dev/null
python3 "${ROOTDIR}/scripts/open-source-license-policy.py" "${audit_root}/sbom.spdx.json"

echo "open_source_check=passed"
