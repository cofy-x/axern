#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
dist="${AXERN_SDK_DIST:-${AXERN_ROOT}/dist/sdk}"

output() {
  printf '%s=%s\n' "$1" "$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$1" "$2" >> "${GITHUB_OUTPUT}"
  fi
}

registry_document() {
  local label="$1"
  local url="$2"
  local destination="$3"
  local status
  status="$(curl --silent --show-error --location --output "${destination}" --write-out '%{http_code}' "${url}")"
  case "${status}" in
    200) return 0 ;;
    404) return 1 ;;
    *)
      echo "cannot verify ${label} ${version}: registry returned HTTP ${status}" >&2
      exit 1
      ;;
  esac
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

pypi_document="${tmp_dir}/pypi.json"
if registry_document "PyPI axern-sdk" "https://pypi.org/pypi/axern-sdk/${version}/json" "${pypi_document}"; then
  python3 - "${pypi_document}" "${dist}/python" <<'PY'
import hashlib
import json
import pathlib
import sys

document = json.loads(pathlib.Path(sys.argv[1]).read_text())
dist = pathlib.Path(sys.argv[2])
remote = {item["filename"]: item["digests"]["sha256"] for item in document["urls"]}
local = {
    path.name: hashlib.sha256(path.read_bytes()).hexdigest()
    for path in dist.iterdir()
    if path.is_file()
}
if remote != local:
    raise SystemExit(f"PyPI axern-sdk files differ from the candidate: remote={remote!r} local={local!r}")
PY
  output publish_python false
else
  output publish_python true
fi

npm_document="${tmp_dir}/npm.json"
if registry_document "npm @cofy-x/axern-sdk" "https://registry.npmjs.org/@cofy-x%2Faxern-sdk/${version}" "${npm_document}"; then
  npm_tarball="${dist}/typescript/cofy-x-axern-sdk-${version}.tgz"
  local_sha1="$(shasum "${npm_tarball}" | awk '{print $1}')"
  remote_sha1="$(jq -er '.dist.shasum' "${npm_document}")"
  if [ "${remote_sha1}" != "${local_sha1}" ]; then
    echo "npm @cofy-x/axern-sdk ${version} differs from the candidate" >&2
    exit 1
  fi
  output publish_typescript false
else
  output publish_typescript true
fi

echo "sdk_release_preflight_ok=${version}"
