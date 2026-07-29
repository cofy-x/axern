#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_SDK_DIST:-${AXERN_ROOT}/dist/sdk}"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"

rm -rf "${dist}"
mkdir -p "${dist}/python" "${dist}/typescript"

uv build --no-sources --out-dir "${dist}/python" "${AXERN_ROOT}/sdk/python"
rm -f "${dist}/python/.gitignore"
pnpm --dir "${AXERN_ROOT}/sdk/typescript" run build
npm pack "${AXERN_ROOT}/sdk/typescript" \
  --pack-destination "${dist}/typescript" \
  --json >/dev/null

python_artifacts=("${dist}/python/axern_sdk-${version}"*)
typescript_artifact="${dist}/typescript/cofy-x-axern-sdk-${version}.tgz"
if [ "${#python_artifacts[@]}" -ne 2 ] || [ ! -f "${typescript_artifact}" ]; then
  echo "SDK artifacts do not match version ${version}" >&2
  exit 1
fi

(
  cd "${dist}"
  find python typescript -type f -print0 | sort -z | xargs -0 shasum -a 256 > checksums.txt
)

echo "sdk_artifacts_dist=${dist}"
