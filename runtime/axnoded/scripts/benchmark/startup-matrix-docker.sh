#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

SCENARIOS_RAW="${STARTUP_MATRIX_SCENARIOS:-runsc-local,runc-local,runsc-oci}"
SCENARIOS_RAW="${SCENARIOS_RAW// /}"
IFS=',' read -r -a SCENARIOS <<<"${SCENARIOS_RAW}"
COLD_SAMPLES="${STARTUP_MATRIX_COLD_SAMPLES:-3}"
WARM_SAMPLES="${STARTUP_MATRIX_WARM_SAMPLES:-10}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="${STARTUP_MATRIX_OUTPUT_DIR:-${ROOT_DIR}/output/startup-matrix-${TIMESTAMP}}"
RAW_DIR="${OUTPUT_DIR}/raw"
SCENARIO_REPORT_DIR="${OUTPUT_DIR}/scenarios"
MATRIX_REPORT="${OUTPUT_DIR}/matrix.json"

mkdir -p "${RAW_DIR}" "${SCENARIO_REPORT_DIR}"
ensure_verify_image

for scenario in "${SCENARIOS[@]}"; do
  [ -n "${scenario}" ] || continue
  scenario_raw_dir="${RAW_DIR}/${scenario}"
  mkdir -p "${scenario_raw_dir}"

  cold_run=1
  while [ "${cold_run}" -le "${COLD_SAMPLES}" ]; do
    echo "startup_matrix_sample scenario=${scenario} mode=cold sample=${cold_run}/${COLD_SAMPLES}" >&2
    STARTUP_MATRIX_SCENARIO="${scenario}" \
    STARTUP_MATRIX_MODE="cold" \
    STARTUP_MATRIX_SAMPLES="1" \
    run_verify_container /bin/bash /workspace/scripts/benchmark/startup-matrix-in-container.sh \
      > "${scenario_raw_dir}/cold-${cold_run}.json"
    cold_run=$((cold_run + 1))
  done

  echo "startup_matrix_sample scenario=${scenario} mode=warm samples=${WARM_SAMPLES}" >&2
  STARTUP_MATRIX_SCENARIO="${scenario}" \
  STARTUP_MATRIX_MODE="warm" \
  STARTUP_MATRIX_SAMPLES="${WARM_SAMPLES}" \
  run_verify_container /bin/bash /workspace/scripts/benchmark/startup-matrix-in-container.sh \
    > "${scenario_raw_dir}/warm.json"

  scenario_report="${SCENARIO_REPORT_DIR}/${scenario}.json"
  GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12}" GOFLAGS="${GOFLAGS:--mod=readonly}" \
    go run ./cmd/natbench-startup-matrix \
      -mode scenario \
      -sample-dir "${scenario_raw_dir}" \
      > "${scenario_report}"

  jq -r --arg scenario "${scenario}" '
    "startup_matrix_scenario scenario=\($scenario) cold_samples=\(.coldSamples) warm_samples=\(.warmSamples) cold_p95=\(.startup.classes.cold.quantiles.p95Seconds // 0) warm_p95=\(.startup.classes.warm.quantiles.p95Seconds // 0) warm_dominant_p95=\(.startup.dominantPhaseP95.warm // "") warm_dominant_p99=\(.startup.dominantPhaseP99.warm // "") bundle_hit_rate=\(.startup.bundle.hitRate // 0) envelope_hits=\(.startup.executionEnvelope.hitCount // 0) envelope_prepared=\(.startup.executionEnvelope.preparedCount // 0)"' \
    "${scenario_report}" >&2
done

GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12}" GOFLAGS="${GOFLAGS:--mod=readonly}" \
  go run ./cmd/natbench-startup-matrix \
    -mode matrix \
    -reports-dir "${SCENARIO_REPORT_DIR}" \
    > "${MATRIX_REPORT}"

echo "startup_matrix_output_dir=${OUTPUT_DIR}" >&2
cat "${MATRIX_REPORT}"
