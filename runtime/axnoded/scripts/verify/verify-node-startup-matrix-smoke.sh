#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
mkdir -p "${ROOT_DIR}/output"
output_dir="$(mktemp -d "${ROOT_DIR}/output/startup-matrix-smoke-XXXXXX")"
cleanup() {
  rm -rf "${output_dir}"
}
trap cleanup EXIT

STARTUP_MATRIX_OUTPUT_DIR="${output_dir}" \
STARTUP_MATRIX_SCENARIOS="runsc-local,runc-local" \
STARTUP_MATRIX_COLD_SAMPLES=1 \
STARTUP_MATRIX_WARM_SAMPLES=2 \
bash "${ROOT_DIR}/scripts/benchmark/startup-matrix-docker.sh" > "${output_dir}/matrix.json"

matrix_json="$(cat "${output_dir}/matrix.json")"
scenario_report="${output_dir}/scenarios/runsc-local.json"
[ -f "${scenario_report}" ] || {
  echo "missing scenario report: ${scenario_report}" >&2
  exit 1
}
scenario_report_runc="${output_dir}/scenarios/runc-local.json"
[ -f "${scenario_report_runc}" ] || {
  echo "missing scenario report: ${scenario_report_runc}" >&2
  exit 1
}

jq -e '
  (.scenarios | length) == 2 and
  (any(.scenarios[]; .scenario == "runsc-local" and .startup.classes.cold != null and .startup.classes.warm != null and .startup.phases != null and .startup.bundle != null and (.startup.bundle.hitCount // 0) > 0 and (.startup.dominantPhaseP95.warm // "") != "" and (.startup.dominantPhaseP99.warm // "") != "")) and
  (any(.scenarios[]; .scenario == "runc-local" and .startup.classes.cold != null and .startup.classes.warm != null and .startup.bundle != null and (.startup.bundle.hitCount // 0) > 0))
' <<<"${matrix_json}" >/dev/null

echo "verify_node_startup_matrix_smoke_ok=true"
