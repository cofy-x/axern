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

cleanup_ids=()

cleanup() {
  local id
  for id in "${cleanup_ids[@]:-}"; do
    if [ -n "${id}" ]; then
      axctl --address "${AXNODED_SOCKET}" sandbox delete "${id}" >/dev/null 2>&1 || true
    fi
  done
}

trap cleanup EXIT

metrics_before="$(metricsz_fetch)"

for runtime_name in runsc runc; do
  runtime_id="bundle-template-${runtime_name}"

  cold_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.bundle-template.first.stdout" "/tmp/${runtime_name}.bundle-template.first.stderr")"
  [ -n "${cold_id}" ] || {
    echo "first start did not return a container id for ${runtime_name}" >&2
    exit 1
  }
  axctl --address "${AXNODED_SOCKET}" sandbox delete "${cold_id}"

  warm_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.bundle-template.second.stdout" "/tmp/${runtime_name}.bundle-template.second.stderr")"
  [ -n "${warm_id}" ] || {
    echo "second start did not return a container id for ${runtime_name}" >&2
    exit 1
  }
  cleanup_ids+=("${warm_id}")
done

metrics_output="$(metricsz_fetch)"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=cold" "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=warm" "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
  "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=miss"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
  "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=hit"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_materialize_duration_seconds" "histogram" "2" \
  "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
  "axern.phase=runtime_bundle_prepare" "axern.start_class=cold" "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
  "axern.phase=runtime_bundle_prepare" "axern.start_class=warm" "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"

metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=cold" "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=warm" "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
  "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=miss"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_template_total" "counter" "1" \
  "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=hit"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_bundle_materialize_duration_seconds" "histogram" "2" \
  "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
  "axern.phase=runtime_bundle_prepare" "axern.start_class=cold" "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=ok"
metricsz_assert_delta "${metrics_before}" "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
  "axern.phase=runtime_bundle_prepare" "axern.start_class=warm" "axern.runtime=runc" "axern.rootfs_type=local" "axern.result=ok"

cleanup
trap - EXIT

echo "verify_node_bundle_template_e2e_ok=true"
