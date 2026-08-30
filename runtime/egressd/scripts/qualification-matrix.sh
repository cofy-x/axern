#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EGRESSD_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
EGRESSD_QUALIFY_BIN="${NETWORK_POLICY_QUALIFICATION_ASSEMBLER:-egressd-qualify}"
SUBJECT_COMMIT_FILE="${NETWORK_POLICY_QUALIFICATION_SUBJECT_COMMIT_FILE:-/usr/local/share/axern/qualification-subject-commit}"

if [ "$(uname -s)" != "Linux" ]; then
  echo "network-policy qualification requires Linux" >&2
  exit 1
fi

DRIVER="${NETWORK_POLICY_QUALIFICATION_DRIVER:-/workspace/scripts/qualification/network-policy-scenario-in-container.sh}"
BUILD_DIGEST="${NETWORK_POLICY_QUALIFICATION_BUILD_DIGEST:?NETWORK_POLICY_QUALIFICATION_BUILD_DIGEST is required}"
HOST_IDENTITY_DIGEST="${NETWORK_POLICY_QUALIFICATION_HOST_IDENTITY_DIGEST:?NETWORK_POLICY_QUALIFICATION_HOST_IDENTITY_DIGEST is required}"
RUNC_BINARY="${NETWORK_POLICY_QUALIFICATION_RUNC_BINARY:-/usr/bin/runc}"
RUNSC_BINARY="${NETWORK_POLICY_QUALIFICATION_RUNSC_BINARY:-/usr/local/bin/runsc}"
SAMPLES="${NETWORK_POLICY_QUALIFICATION_SAMPLES:-20}"
CONCURRENCY="${NETWORK_POLICY_QUALIFICATION_CONCURRENCY:-16}"
PAYLOAD_BYTES="${NETWORK_POLICY_QUALIFICATION_PAYLOAD_BYTES:-1048576}"
SUSTAINED_SECONDS="${NETWORK_POLICY_QUALIFICATION_SUSTAINED_SECONDS:-60}"
RULE_SCALE_COUNTS="${NETWORK_POLICY_QUALIFICATION_RULE_SCALE_COUNTS:-1,64,256}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUTPUT_DIR="${NETWORK_POLICY_QUALIFICATION_OUTPUT_DIR:-/qualification-output/network-policy-qualification-${TIMESTAMP}}"
SCENARIO_DIR="${OUTPUT_DIR}/scenarios"
REPORT_PATH="${OUTPUT_DIR}/report.json"
COMPARISON_PATH="${OUTPUT_DIR}/comparison.json"

if [ ! -x "${DRIVER}" ]; then
  echo "qualification driver is not executable: ${DRIVER}" >&2
  exit 1
fi
if [ ! -x "${RUNC_BINARY}" ] || [ ! -x "${RUNSC_BINARY}" ]; then
  echo "both runc and runsc qualification binaries must be executable" >&2
  exit 1
fi

if [ ! -s "${SUBJECT_COMMIT_FILE}" ]; then
  echo "qualification runner has no embedded subject commit: ${SUBJECT_COMMIT_FILE}" >&2
  exit 1
fi
SUBJECT_COMMIT="$("${EGRESSD_QUALIFY_BIN}" subject -file "${SUBJECT_COMMIT_FILE}")"

mkdir -p "${SCENARIO_DIR}"

runtimes=(runc runsc)
backends=(bridge ebpf)
families=(ipv4 ipv6)
modes=(unrestricted dns_deny strict_domain strict_cidr)

for runtime_name in "${runtimes[@]}"; do
  for backend in "${backends[@]}"; do
    for family in "${families[@]}"; do
      for mode in "${modes[@]}"; do
        scenario="${runtime_name}-${backend}-${family}-${mode}"
        output="${SCENARIO_DIR}/${scenario}.json"
        echo "network_policy_qualification_scenario=${scenario}" >&2
        "${DRIVER}" \
          --runtime "${runtime_name}" \
          --network-backend "${backend}" \
          --ip-family "${family}" \
          --policy-mode "${mode}" \
          --samples "${SAMPLES}" \
          --concurrency "${CONCURRENCY}" \
          --payload-bytes "${PAYLOAD_BYTES}" \
          --sustained-seconds "${SUSTAINED_SECONDS}" \
          --rule-scale-counts "${RULE_SCALE_COUNTS}" \
          --output "${output}"
        if [ ! -s "${output}" ]; then
          echo "qualification driver did not write ${output}" >&2
          exit 1
        fi
      done
    done
  done
done

"${EGRESSD_QUALIFY_BIN}" assemble \
  -scenarios "${SCENARIO_DIR}" \
  -subject-commit "${SUBJECT_COMMIT}" \
  -subject-build-digest "${BUILD_DIGEST}" \
  -host-identity-digest "${HOST_IDENTITY_DIGEST}" \
  -runc-binary "${RUNC_BINARY}" \
  -runsc-binary "${RUNSC_BINARY}" \
  -samples "${SAMPLES}" \
  -concurrency "${CONCURRENCY}" \
  -payload-bytes "${PAYLOAD_BYTES}" \
  -sustained-seconds "${SUSTAINED_SECONDS}" \
  -rule-scale-counts "${RULE_SCALE_COUNTS}" \
  >"${REPORT_PATH}"

"${EGRESSD_QUALIFY_BIN}" validate -report "${REPORT_PATH}" -full-matrix=true >&2

if [ -n "${NETWORK_POLICY_QUALIFICATION_BASELINE:-}" ]; then
  "${EGRESSD_QUALIFY_BIN}" compare \
    -baseline "${NETWORK_POLICY_QUALIFICATION_BASELINE}" \
    -candidate "${REPORT_PATH}" \
    -budget "${EGRESSD_DIR}/qualification/budget.json" \
    >"${COMPARISON_PATH}"
  echo "network_policy_qualification_comparison=${COMPARISON_PATH}" >&2
fi

echo "network_policy_qualification_report=${REPORT_PATH}" >&2
cat "${REPORT_PATH}"
