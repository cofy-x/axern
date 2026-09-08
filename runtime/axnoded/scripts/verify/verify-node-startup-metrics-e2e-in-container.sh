#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"

for _ in $(seq 1 40); do
  if [ -S "${AXNODED_SOCKET}" ] && curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${AXNODED_SOCKET}" ] || ! curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
  echo "axnoded readiness not ready" >&2
  exit 1
fi

metricsz_wait_capability_snapshot

start_container() {
  local runtime_name="$1"
  local runtime_id="$2"
  local stdout_path="$3"
  local stderr_path="$4"

  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -runtime-id "${runtime_id}" \
    -stdout "${stdout_path}" \
    -stderr "${stderr_path}" \
    -shell-command "sleep 300" \
  | awk -F= '/^container_id=/{print $2}'
}

metrics_before="$(metricsz_fetch)"

for runtime_name in runsc runc; do
  runtime_id="startup-metrics-${runtime_name}"

  cold_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.cold.stdout" "/tmp/${runtime_name}.cold.stderr")"
  [ -n "${cold_id}" ] || {
    echo "cold start did not return a container id for ${runtime_name}" >&2
    exit 1
  }
  axctl --address "${AXNODED_SOCKET}" sandbox delete "${cold_id}"

  warm_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.warm.stdout" "/tmp/${runtime_name}.warm.stderr")"
  [ -n "${warm_id}" ] || {
    echo "warm start did not return a container id for ${runtime_name}" >&2
    exit 1
  }
  axctl --address "${AXNODED_SOCKET}" sandbox delete "${warm_id}"
done

for runtime_name in runsc runc; do
  metricsz_wait_delta "${metrics_before}" "axern.axnoded_bundle_template_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=hit"
  metricsz_wait_delta "${metrics_before}" "axern.axnoded_bundle_materialize_duration_seconds" "histogram" "2" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"

  metrics_output="$(metricsz_fetch)"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
    "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
    "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=langruntime_lookup" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=rootfs_prepare" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=resource_allocate" "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=egress_policy_prepare" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=egress_policy_prepare" "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=runtime_bundle_prepare" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=runtime_launch" "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  # The cold start prepares the template and the warm start reuses it. Retaining
  # the runtime after deletion does not materialize another bundle.
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=miss"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=hit"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_materialize_duration_seconds" "histogram" "2" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
done

# The final deletes leave one shared local rootfs retained and one idle runtime
# retained for each runtime backend.
metricsz_wait_value "axern.axnoded_retained_runtime_current" "gauge" "2" \
  "axern.rootfs_type=local"
metricsz_wait_value "axern.axnoded_retained_rootfs_current" "gauge" "1" \
  "axern.rootfs_type=local"
metrics_output="$(metricsz_fetch)"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retained_runtime_current" "gauge" "2" \
  "axern.rootfs_type=local"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retained_rootfs_current" "gauge" "1" \
  "axern.rootfs_type=local"

echo "verify_node_startup_metrics_e2e_ok=true"
