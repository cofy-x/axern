#!/usr/bin/env bash
set -euo pipefail

output_dir=${AXERN_SOAK_OUTPUT_DIR:-/results}
model=${AXERN_SOAK_LOAD_MODEL:-steady}
mkdir -p "${output_dir}"

case "${model}" in
  steady)
    policy=${AXERN_SOAK_SLO_POLICY:-/opt/axern/startup-readiness/mixed-high-water-slo.json}
    python /opt/axern/startup-readiness/steady_soak.py
    python /opt/axern/startup-readiness/evaluate_steady_slo.py \
      --policy "${policy}" \
      --input "${output_dir}/steady.jsonl" \
      | tee "${output_dir}/slo-result.json"
    ;;
  cohort)
    policy=${AXERN_SOAK_SLO_POLICY:-/opt/axern/startup-readiness/mixed-warm-soak-slo.json}
    python /opt/axern/startup-readiness/lifecycle_soak.py
    inputs=()
    while IFS= read -r path; do
      inputs+=(--input "${path}")
    done < <(find "${output_dir}" -maxdepth 1 -type f -name 'cohort-*.jsonl' | sort)
    if [[ "${#inputs[@]}" -eq 0 ]]; then
      echo "error: soak did not produce cohort results" >&2
      exit 1
    fi
    python /opt/axern/startup-readiness/evaluate_slo.py \
      --policy "${policy}" \
      "${inputs[@]}" \
      | tee "${output_dir}/slo-result.json"
    ;;
  *)
    echo "error: AXERN_SOAK_LOAD_MODEL must be steady or cohort" >&2
    exit 2
    ;;
esac
touch "${output_dir}/completed"
