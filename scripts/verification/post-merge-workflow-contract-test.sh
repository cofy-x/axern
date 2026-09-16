#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW="${ROOT_DIR}/.github/workflows/post-merge-full.yml"

required=(
  "name: Post-Merge Full"
  "workflow_dispatch:"
  "push:"
  "- main"
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

if grep -Eq '^[[:space:]]+pull_request:' "${WORKFLOW}"; then
  echo "post-merge full regression must not run as a pull-request gate" >&2
  exit 1
fi

if grep -Eq '^[[:space:]]+(APT_MIRROR_SOURCE|CARGO_REGISTRY_SOURCE|GOPROXY):' "${WORKFLOW}"; then
  echo "hosted regression must use upstream defaults, not regional mirrors" >&2
  exit 1
fi

bash "${ROOT_DIR}/scripts/verification/verify-all-test.sh"

if grep -Eq '^[[:space:]]+(checks|contents|packages|pull-requests): write' "${WORKFLOW}"; then
  echo "post-merge full regression must retain read-only repository permissions" >&2
  exit 1
fi

printf 'post_merge_workflow_contract_ok=true\n'

# The required Go check must cover Linux-only node packages before merge.
# The host-safe subset and network traffic smoke do not execute this suite.
python3 - "${ROOT_DIR}/.github/workflows/ci.yml" <<'PY'
import pathlib
import re
import sys

source = pathlib.Path(sys.argv[1]).read_text()
job = re.search(r"^  go:\n(.*?)(?=^  [a-z][a-z-]*:|\Z)", source, re.M | re.S)
assert job, "missing required Go job"
assert "runs-on: ubuntu-latest" in job.group(1), "Go job must execute on Linux"
assert "libprotobuf-dev protobuf-compiler ripgrep jq" in job.group(1), "Linux tests require explicit host utilities"
assert re.search(r"^        run: make axnoded-test$", job.group(1), re.M), "Go job omits full Linux node tests"
print("pre_merge_linux_node_contract_ok=true")
PY
