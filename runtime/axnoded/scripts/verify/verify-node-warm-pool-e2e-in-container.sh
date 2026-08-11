#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"

declare -a container_ids=()

cleanup() {
  for container_id in "${container_ids[@]}"; do
    if [ -n "${container_id}" ]; then
      axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}" >/dev/null 2>&1 || true
    fi
  done
}
trap cleanup EXIT

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

metricsz_wait_platform_capability_available "PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT"

start_container() {
  local index="$1"
  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime runsc \
    -runtime-id "warm-pool-runsc-${index}" \
    -stdout "/tmp/warm-pool.${index}.stdout" \
    -stderr "/tmp/warm-pool.${index}.stderr" \
    -shell-command "sleep 300" \
    -request-cpu-milli 250 \
    -request-memory-mib 128 \
    -limit-cpu-milli 500 \
    -limit-memory-mib 256 \
  | awk -F= '/^container_id=/{print $2}'
}

declare -a pid_files=()
declare -a id_files=()

metricsz_wait_at_least "axern.axnoded_resource_pool_idle_current" "gauge" "1" "axern.resource=interface"
metricsz_wait_at_least "axern.axnoded_resource_pool_idle_current" "gauge" "1" "axern.resource=cgroup"

for index in 1 2 3 4; do
  id_files+=("/tmp/warm-pool.${index}.id")
  (
    start_container "${index}" >"${id_files[$((index-1))]}"
  ) &
  pid_files+=("$!")
done

for pid in "${pid_files[@]}"; do
  wait "${pid}"
done

for id_file in "${id_files[@]}"; do
  container_id="$(tr -d '\n' <"${id_file}")"
  if [ -z "${container_id}" ]; then
    echo "missing container id from ${id_file}" >&2
    exit 1
  fi
  container_ids+=("${container_id}")
done

sleep 2

metrics_output="$(metricsz_fetch)"
metricsz_assert_at_least "${metrics_output}" "axern.axnoded_resource_pool_allocate_total" "counter" "1" \
  "axern.resource=interface" "axern.result=miss_sync_create"
metricsz_assert_at_least "${metrics_output}" "axern.axnoded_resource_pool_idle_current" "gauge" "0" \
  "axern.resource=interface"
metricsz_assert_at_least "${metrics_output}" "axern.axnoded_resource_pool_target_current" "gauge" "1" \
  "axern.resource=interface"
metricsz_assert_at_least "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
  "axern.phase=resource_allocate" "axern.start_class=cold" "axern.runtime=runsc" "axern.rootfs_type=local" "axern.result=ok"

echo "verify_node_warm_pool_e2e_ok=true"
