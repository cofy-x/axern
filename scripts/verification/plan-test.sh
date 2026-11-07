#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PLAN="${ROOT_DIR}/scripts/verification/plan.sh"
work_dir=$(mktemp -d)
trap 'rm -rf "${work_dir}"' EXIT

assert_plan() {
  local name=$1 path=$2 expected_fast=$3 expected_network=$4 expected_release=${5:-false}
  local paths_file="${work_dir}/${name}.paths" output_file="${work_dir}/${name}.out"
  printf '%s\n' "${path}" >"${paths_file}"
  "${PLAN}" --paths-file "${paths_file}" --plan >"${output_file}"
  grep -Fx "verification.fast=${expected_fast}" "${output_file}" >/dev/null
  grep -Fx "verification.network_policy_linux=${expected_network}" "${output_file}" >/dev/null
  grep -Fx "verification.release_contract=${expected_release}" "${output_file}" >/dev/null
}

assert_plan docs docs/architecture/runtime-architecture.md docs false
assert_plan egress runtime/egressd/internal/enforcement/nft.go egressd true
assert_plan axnoded-volume runtime/axnoded/internal/volume/store.go axnoded false
assert_plan migration control/controld/internal/postgres/migrations/000005_example.sql go false
assert_plan agent-dockerfile images/agents/Dockerfile agent-images false
assert_plan agent-python images/agents/build.py agent-images false
assert_plan agent-lock images/agents/versions.lock.json agent-images false
assert_plan agent-tests images/agents/test_images.py agent-images false
assert_plan agent-docs images/agents/README.md docs,agent-images false
assert_plan agent-make mk/agent-images.mk agent-images false
assert_plan python-sdk sdk/python/src/axern_sdk/client.py python false
assert_plan unknown-image images/unknown/build.py verify-fast-all true
assert_plan classifier scripts/verification/plan.sh verify-fast-all,release-contract true true
assert_plan classifier-test scripts/verification/plan-test.sh verify-fast-all,release-contract true true
assert_plan release-workflow .github/workflows/release.yml verify-fast-all,release-contract true true
assert_plan post-merge-workflow .github/workflows/post-merge-full.yml verify-fast-all,release-contract true true
assert_plan ci-workflow .github/workflows/ci.yml verify-fast-all,release-contract true true
assert_plan build-cache scripts/dev-env/docker-build-cache.sh verify-fast-all,release-contract true true
assert_plan full-gate scripts/verify-all.sh verify-fast-all,release-contract true true

# Execute the actual group selection with an inert make function. This catches
# a planner which prints the right scope but runs SDK or Docker checks anyway.
make() { printf 'mock_make=%s\n' "$*"; }
export -f make
printf '%s\n' images/agents/build.py >"${work_dir}/agent-execution.paths"
"${PLAN}" --paths-file "${work_dir}/agent-execution.paths" >"${work_dir}/agent-execution.out"
grep -Fx 'mock_make=agent-images-check' "${work_dir}/agent-execution.out" >/dev/null
if grep -Eq '^mock_make=.*(test-py|lint-py|agent-images-smoke|verify-fast-all)' "${work_dir}/agent-execution.out"; then
  echo 'workload image checks escaped their host-safe owner' >&2
  exit 1
fi

# Execute the Make wrappers with an inert interpreter to verify their argv
# boundary. Spaces and shell punctuation in caller paths must stay data.
cat >"${work_dir}/mock python" <<'PY'
#!/usr/bin/env python3
import json
import os
import sys
with open(os.environ["AGENT_MAKE_RECEIPT"], "a") as receipt:
    receipt.write(json.dumps(sys.argv[1:]) + "\n")
PY
chmod +x "${work_dir}/mock python"
export AGENT_MAKE_RECEIPT="${work_dir}/agent-make.jsonl"
command make --no-print-directory -s -C "${ROOT_DIR}" agent-images agent-images-check agent-images-smoke \
  "AGENT_IMAGE_PYTHON=${work_dir}/mock python" \
  'AGENT_IMAGE_TAG_PREFIX=registry.example.test/team;echo' \
  'AGENT_IMAGE_TAG_SUFFIX=contract' \
  "AGENT_IMAGE_CACHE_DIR=${work_dir}/cache with spaces" \
  "AGENT_IMAGE_DOCKER_CONFIG=${work_dir}/docker with spaces" \
  "AGENT_IMAGE_OUTPUT_DIR=${work_dir}/output with spaces"
python3 - "${ROOT_DIR}" "${work_dir}" <<'PY'
import json
import pathlib
import sys
root, scratch = map(pathlib.Path, sys.argv[1:])
build, check, smoke = [json.loads(line) for line in (scratch / "agent-make.jsonl").read_text().splitlines()]
common = ["--targets", "base,toolchain,claude-code,codex", "--tag-prefix",
          "registry.example.test/team;echo", "--tag-suffix", "contract"]
paths = ["--docker-config", str(scratch / "docker with spaces"),
         "--output-dir", str(scratch / "output with spaces")]
assert build == [str(root / "images/agents/build.py"), *common,
                 "--cache-dir", str(scratch / "cache with spaces"), *paths], build
assert check == [str(root / "images/agents/check.py")], check
assert smoke == [str(root / "images/agents/smoke.py"), *common, *paths], smoke
PY

github_output="${work_dir}/github.out"
paths_file="${work_dir}/empty.paths"
: >"${paths_file}"
"${PLAN}" --paths-file "${paths_file}" --plan --all-heavy --github-output "${github_output}" >/dev/null
grep -Fx 'network_policy_linux=true' "${github_output}" >/dev/null
grep -Fx 'release_contract=true' "${github_output}" >/dev/null

printf 'verification_plan_contract_ok=true\n'
