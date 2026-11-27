#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW="${ROOT_DIR}/.github/workflows/post-merge-full.yml"

required=(
  "name: Post-Merge Full"
  "contents: read"
  "group: post-merge-full-\${{ github.ref }}"
  "cancel-in-progress: true"
  "name: Full Repository Regression"
  "suite: [source, runtime]"
  "fail-fast: false"
  'needs: full-regression'
  "run: test \"\${RESULT}\" = success"
  'VERIFY_TIMINGS_FILE:'
  "runs-on: ubuntu-24.04"
  "timeout-minutes: 180"
  "AXERN_DOCKER_CACHE_BACKEND: gha"
  "AXERN_NODE_RUNTIME_BASE_CACHE_BACKEND: gha"
  "libprotobuf-dev protobuf-compiler ripgrep jq"
  "make verify-full"
  "if: failure()"
  "if: always()"
  "retention-days: 14"
)

for expected in "${required[@]}"; do
  if ! grep -Fq -- "${expected}" "${WORKFLOW}"; then
    printf 'post-merge workflow is missing required contract: %s\n' "${expected}" >&2
    exit 1
  fi
done

if grep -Eq '^[[:space:]]+(APT_MIRROR_SOURCE|GOPROXY):' "${WORKFLOW}"; then
  echo "hosted regression must use upstream defaults, not regional mirrors" >&2
  exit 1
fi

bash "${ROOT_DIR}/scripts/verification/verify-all-test.sh"

if grep -Eq '^[[:space:]]+(checks|contents|packages|pull-requests): write' "${WORKFLOW}"; then
  echo "post-merge full regression must retain read-only repository permissions" >&2
  exit 1
fi

printf 'post_merge_workflow_contract_ok=true\n'

# Optional hosted diagnostics still cover the full Linux node suite.
python3 - "${ROOT_DIR}/.github/workflows" <<'PY'
import pathlib
import re
import sys

workflows = pathlib.Path(sys.argv[1])
for name in ("ci", "dco", "post-merge-full", "docs-pages-deploy"):
    source = (workflows / f"{name}.yml").read_text()
    trigger_section = re.search(r"^on:\n(.*?)(?=^\S|\Z)", source, re.M | re.S)
    assert trigger_section, f"missing workflow triggers: {name}"
    assert re.findall(r"^  ([a-z_]+):", trigger_section[1], re.M) == ["workflow_dispatch"], (
        f"{name} must run only when explicitly dispatched"
    )

source = (workflows / "ci.yml").read_text()
assert "run: make axnoded-verify-network-policy-linux-matrix" in source, "CI omits the full network matrix"
job = re.search(r"^  go:\n(.*?)(?=^  [a-z][a-z-]*:|\Z)", source, re.M | re.S)
assert job, "missing Go job"
assert "runs-on: ubuntu-latest" in job.group(1), "Go job must execute on Linux"
assert "libprotobuf-dev protobuf-compiler ripgrep jq" in job.group(1), "Linux tests require explicit host utilities"
assert re.search(r"^        run: make axnoded-test$", job.group(1), re.M), "Go job omits full Linux node tests"
print("manual_verification_workflow_contract_ok=true")
PY
