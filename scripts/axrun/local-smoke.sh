#!/usr/bin/env bash
set -euo pipefail

axern_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
work_root="$(mktemp -d)"
cleanup() { rm -rf "${work_root}"; }
trap cleanup EXIT

make -C "${axern_root}" axrun-build >/dev/null
axrun="${axern_root}/bin/axrun"
"${axrun}" task init --output-dir "${work_root}/source"
"${axrun}" task build --file "${work_root}/source/taskset.yaml" --output "${work_root}/bundle"
"${axrun}" task inspect "${work_root}/bundle"

# The local gate owns deterministic compilation. Durable rollout execution is
# covered by the mandatory Compose gate, which supplies the real control-plane
# topology and the test-only provider. Do not resurrect the removed top-level
# run command as a local compatibility path.
"${axrun}" task build --file "${work_root}/source/taskset.yaml" --output "${work_root}/bundle-repeat"
cmp "${work_root}/bundle/descriptor.json" "${work_root}/bundle-repeat/descriptor.json"
cmp "${work_root}/bundle/payload.tar" "${work_root}/bundle-repeat/payload.tar"
test -s "${work_root}/bundle/build.json"
test -s "${work_root}/bundle/oci/index.json"

echo "axrun_local_smoke_ok=true"
