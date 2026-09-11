#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PLAN="${ROOT_DIR}/scripts/verification/plan.sh"
work_dir=$(mktemp -d)
trap 'rm -rf "${work_dir}"' EXIT

assert_plan() {
  local name=$1 path=$2 expected_fast=$3 expected_network=$4 expected_rollout=$5
  local paths_file="${work_dir}/${name}.paths" output_file="${work_dir}/${name}.out"
  printf '%s\n' "${path}" >"${paths_file}"
  "${PLAN}" --paths-file "${paths_file}" --plan >"${output_file}"
  grep -Fx "verification.fast=${expected_fast}" "${output_file}" >/dev/null
  grep -Fx "verification.network_policy_linux=${expected_network}" "${output_file}" >/dev/null
  grep -Fx "verification.managed_rollout=${expected_rollout}" "${output_file}" >/dev/null
}

assert_plan docs docs/architecture/runtime-architecture.md docs false false
assert_plan module-doc apps/axrun/docs/architecture.md docs false false
assert_plan egress runtime/egressd/internal/enforcement/nft.go egressd true false
assert_plan axnoded-volume runtime/axnoded/internal/volume/store.go axnoded false false
assert_plan rollout apps/axrun/internal/application/rollout/execute.go go false true
assert_plan migration control/controld/internal/postgres/migrations/000005_example.sql go false true
assert_plan classifier scripts/verification/plan.sh verify-fast-all true true
assert_plan release-workflow .github/workflows/release.yml verify-fast-all,release-contract true true

github_output="${work_dir}/github.out"
paths_file="${work_dir}/empty.paths"
: >"${paths_file}"
"${PLAN}" --paths-file "${paths_file}" --plan --all-heavy --github-output "${github_output}" >/dev/null
grep -Fx 'network_policy_linux=true' "${github_output}" >/dev/null
grep -Fx 'managed_rollout=true' "${github_output}" >/dev/null
grep -Fx 'release_contract=true' "${github_output}" >/dev/null

printf 'verification_plan_contract_ok=true\n'
