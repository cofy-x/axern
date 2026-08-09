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
metrics_before="$(metricsz_fetch)"

start_container() {
  local runtime_name="$1"
  local runtime_id="$2"
  shift 2
  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -runtime-id "${runtime_id}" \
    "$@" \
  | awk -F= '/^container_id=/{print $2}'
}

verify_runtime() {
  local runtime_name="$1"
  local runtime_id="execution-envelope-${runtime_name}"
  local cold_id=""
  local warm_hit_id=""
  local metrics_output=""

  cold_id="$(start_container "${runtime_name}" "${runtime_id}" -stdout "" -stderr "" -shell-command "sleep 300" -allocation-attempt 0)"
  [ -n "${cold_id}" ] || {
    echo "first ${runtime_name} start did not return a container id" >&2
    exit 1
  }
  axctl --address "${AXNODED_SOCKET}" sandbox delete --timeout 0s "${cold_id}"

  metricsz_wait_delta "${metrics_before}" "axern.axnoded_execution_envelope_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=prepared"
  metricsz_wait_value "axern.axnoded_execution_envelope_current" "gauge" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.state=ready"

  warm_hit_id="$(start_container "${runtime_name}" "${runtime_id}" -stdout "" -stderr "" -shell-command "sleep 300" -allocation-attempt 0)"
  [ -n "${warm_hit_id}" ] || {
    echo "second ${runtime_name} start did not return a container id" >&2
    exit 1
  }
  local dynamic_container_id=""
  dynamic_container_id="$(start_container "${runtime_name}" "${runtime_id}" -stdout "" -stderr "" -shell-command "sleep 300" -request-cpu-milli 250 -allocation-attempt 0)"
  [ -n "${dynamic_container_id}" ] || {
    echo "dynamic ${runtime_name} fallback start did not return a container id" >&2
    exit 1
  }

  metrics_output="$(metricsz_fetch)"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
    "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "2" \
    "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=prepared"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=hit"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=miss"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_total" "counter" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=fallback"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_prepare_duration_seconds" "histogram" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_execution_envelope_activate_duration_seconds" "histogram" "1" \
    "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
  metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "2" \
    "axern.phase=runtime_launch" "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=local" "axern.result=ok"
}

verify_runtime "runsc"
verify_runtime "runc"

echo "verify_node_execution_envelope_prewarm_e2e_ok=true"
