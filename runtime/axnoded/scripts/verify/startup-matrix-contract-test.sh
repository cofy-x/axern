#!/usr/bin/env bash
set -euo pipefail

AXNODED_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

python3 - "${AXNODED_DIR}/scripts/verify/verify-node-startup-matrix-smoke.sh" <<'PY'
import pathlib
import sys

smoke = pathlib.Path(sys.argv[1]).read_text()

required = (
    'bash "${ROOT_DIR}/scripts/benchmark/startup-matrix-docker.sh" >/dev/null',
    'matrix_json="$(cat "${output_dir}/matrix.json")"',
)
for fragment in required:
    if fragment not in smoke:
        raise SystemExit(f"startup matrix smoke is missing output isolation contract: {fragment}")

if 'startup-matrix-docker.sh" > "${output_dir}/matrix.json"' in smoke:
    raise SystemExit("startup matrix smoke must not redirect stdout over the persisted matrix report")
PY

echo "startup_matrix_contract_ok=true"
