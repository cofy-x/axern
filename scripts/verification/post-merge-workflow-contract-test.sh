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
  "runs-on: ubuntu-24.04"
  "timeout-minutes: 180"
  "AXERN_DOCKER_CACHE_BACKEND: gha"
  "AXERN_NODE_RUNTIME_BASE_CACHE_BACKEND: gha"
  "protobuf-compiler ripgrep"
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

if grep -Eq '^[[:space:]]+(checks|contents|packages|pull-requests): write' "${WORKFLOW}"; then
  echo "post-merge full regression must retain read-only repository permissions" >&2
  exit 1
fi

printf 'post_merge_workflow_contract_ok=true\n'
