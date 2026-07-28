#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GENERATED_PATHS=(
  "sdk/go/gen"
  "sdk/python/src/axern"
  "runtime/axnoded/internal/apipb"
)

cd "${ROOT_DIR}"

before="$(mktemp)"
after="$(mktemp)"
trap 'rm -f "${before}" "${after}"' EXIT

snapshot_generated_state() {
  git status --short --untracked-files=all -- "${GENERATED_PATHS[@]}"
  git diff --binary -- "${GENERATED_PATHS[@]}"
}

snapshot_generated_state > "${before}"
bash scripts/proto-generate.sh
snapshot_generated_state > "${after}"

if cmp -s "${before}" "${after}"; then
  echo "proto_generated_check_ok=true"
  exit 0
fi

echo "proto generated outputs changed after regeneration." >&2
echo "Run 'make protos' and commit the generated outputs." >&2
git status --short --untracked-files=all -- "${GENERATED_PATHS[@]}" >&2
git diff --stat -- "${GENERATED_PATHS[@]}" >&2
exit 1
