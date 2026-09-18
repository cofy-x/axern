#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PLAN="${ROOT_DIR}/scripts/verification/plan.sh"
work_dir=$(mktemp -d)
trap 'rm -rf "${work_dir}"' EXIT

assert_plan() {
  local name=$1 path=$2 expected_fast=$3 expected_network=$4
  local paths_file="${work_dir}/${name}.paths" output_file="${work_dir}/${name}.out"
  printf '%s\n' "${path}" >"${paths_file}"
  "${PLAN}" --paths-file "${paths_file}" --plan >"${output_file}"
  grep -Fx "verification.fast=${expected_fast}" "${output_file}" >/dev/null
  grep -Fx "verification.network_policy_linux=${expected_network}" "${output_file}" >/dev/null
}

assert_plan docs docs/architecture/runtime-architecture.md docs false
assert_plan egress runtime/egressd/internal/enforcement/nft.go egressd true
assert_plan axnoded-volume runtime/axnoded/internal/volume/store.go axnoded false
assert_plan migration control/controld/internal/postgres/migrations/000005_example.sql go false
assert_plan classifier scripts/verification/plan.sh verify-fast-all,release-contract true
assert_plan release-workflow .github/workflows/release.yml verify-fast-all,release-contract true
assert_plan post-merge-workflow .github/workflows/post-merge-full.yml verify-fast-all,release-contract true
assert_plan ci-workflow .github/workflows/ci.yml verify-fast-all,release-contract true
assert_plan build-cache scripts/dev-env/docker-build-cache.sh verify-fast-all,release-contract true
assert_plan full-gate scripts/verify-all.sh verify-fast-all,release-contract true

github_output="${work_dir}/github.out"
paths_file="${work_dir}/empty.paths"
: >"${paths_file}"
"${PLAN}" --paths-file "${paths_file}" --plan --all-heavy --github-output "${github_output}" >/dev/null
grep -Fx 'network_policy_linux=true' "${github_output}" >/dev/null
grep -Fx 'release_contract=true' "${github_output}" >/dev/null

printf 'verification_plan_contract_ok=true\n'
