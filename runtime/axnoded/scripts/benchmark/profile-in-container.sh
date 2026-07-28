#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/lib/ebpf-ingress-probe.sh"
. "${ROOT_DIR}/scripts/lib/node-runtime-services.sh"

RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST:-runsc}"
RUNTIME_BINARY="${RUNTIME_BINARY:-/usr/local/bin/${RUNTIME_UNDER_TEST}}"
SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
AXNODED_BIN="${AXNODED_BIN:-/usr/local/bin/axnoded}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
DEFAULT_UPLINK="${DEFAULT_UPLINK:-$(ip route show default | awk '/default/ {print $5; exit}')}"
BPFNET_PIN_PATH="${BPFNET_PIN_PATH:-/sys/fs/bpf/axern/bpfnet}"
BPFNET_MAP_SIZE="${BPFNET_MAP_SIZE:-16384}"
BPFNET_SNAT_MAP_SIZE="${BPFNET_SNAT_MAP_SIZE:-262144}"
BPFNET_SNAT_GC_INTERVAL="${BPFNET_SNAT_GC_INTERVAL:-1s}"
BPFNET_SNAT_TCP_IDLE_TIMEOUT="${BPFNET_SNAT_TCP_IDLE_TIMEOUT:-5m}"
BPFNET_SNAT_TCP_CLOSING_TIMEOUT="${BPFNET_SNAT_TCP_CLOSING_TIMEOUT:-2s}"
BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT="${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT:-10s}"
AXNODED_IP_RANGE="${AXNODED_IP_RANGE:-172.31.0.1/16}"
BENCHMARK_REQUESTS="${BENCHMARK_REQUESTS:-1000}"
BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY:-16}"
BENCHMARK_WARMUP_REQUESTS="${BENCHMARK_WARMUP_REQUESTS:-64}"
BENCHMARK_PATHS="${BENCHMARK_PATHS:-egress_udp,egress_udp_connected}"
BENCHMARK_PROFILE_MODE="${BENCHMARK_PROFILE_MODE:-stat}"
BENCHMARK_PROFILE_EVENTS="${BENCHMARK_PROFILE_EVENTS:-}"
BENCHMARK_PROFILE_RETRIES="${BENCHMARK_PROFILE_RETRIES:-3}"
DEFAULT_PERF_STAT_EVENTS="task-clock,context-switches,cpu-migrations,page-faults"
DEFAULT_PERF_RECORD_EVENT="cpu-clock"

setup_node_runtime_volume_defaults
ensure_bpf_fs "${NAT_BACKEND}"

require_perf_ready() {
  if ! command -v perf >/dev/null 2>&1; then
    echo "perf profiling requires a real Linux truth environment with perf installed inside the verify image" >&2
    return 1
  fi

  local kernel
  kernel="$(uname -r)"
  local check_stderr="/tmp/perf-check.stderr"
  local check_out="/tmp/perf-check.out"
  rm -f "${check_stderr}" "${check_out}"
  if perf stat -e task-clock -o "${check_out}" -- sleep 0.01 >/dev/null 2>"${check_stderr}"; then
    return 0
  fi

  local reason=""
  if [ -s "${check_stderr}" ]; then
    reason="$(tr '\n' ' ' <"${check_stderr}" | sed 's/[[:space:]]\+/ /g')"
  fi
  echo "perf profiling requires a real Linux truth environment with matching perf tooling; kernel=${kernel} mode=${BENCHMARK_PROFILE_MODE} reason=${reason:-unknown}" >&2
  return 1
}

profile_command() {
  local output_file="$1"
  local benchmark_file="$2"

  case "${BENCHMARK_PROFILE_MODE}" in
    stat)
      local events="${BENCHMARK_PROFILE_EVENTS:-${DEFAULT_PERF_STAT_EVENTS}}"
      perf stat -e "${events}" -x, -o "${output_file}" \
        -- bash "${ROOT_DIR}/scripts/benchmark/benchmark-runsc-profile.sh" >"${benchmark_file}"
      ;;
    record)
      local events="${BENCHMARK_PROFILE_EVENTS:-${DEFAULT_PERF_RECORD_EVENT}}"
      perf record -e "${events}" --call-graph fp -o "${output_file}" \
        -- bash "${ROOT_DIR}/scripts/benchmark/benchmark-runsc-profile.sh" >"${benchmark_file}"
      perf report --stdio --no-children -i "${output_file}" > /tmp/profile-perf-report.txt
      ;;
    *)
      echo "unsupported BENCHMARK_PROFILE_MODE=${BENCHMARK_PROFILE_MODE} (expected stat or record)" >&2
      return 1
      ;;
  esac
}

is_retryable_profile_error() {
  local stderr_file="$1"
  if [ ! -s "${stderr_file}" ]; then
    return 1
  fi
  grep -q "exit status unavailable" "${stderr_file}"
}

run_profile_with_retries() {
  local output_file="$1"
  local benchmark_file="$2"
  local stderr_file="/tmp/profile-command.stderr"
  local attempt=1

  while [ "${attempt}" -le "${BENCHMARK_PROFILE_RETRIES}" ]; do
    rm -f "${stderr_file}"
    if profile_command "${output_file}" "${benchmark_file}" 2>"${stderr_file}"; then
      rm -f "${stderr_file}"
      return 0
    fi

    if [ "${attempt}" -lt "${BENCHMARK_PROFILE_RETRIES}" ] && is_retryable_profile_error "${stderr_file}"; then
      echo "profile_retry attempt=${attempt}/${BENCHMARK_PROFILE_RETRIES} reason=exit_status_unavailable" >&2
      attempt=$((attempt + 1))
      sleep 1
      continue
    fi

    cat "${stderr_file}" >&2
    return 1
  done

  return 1
}

if [ "${RUNTIME_UNDER_TEST}" != "runsc" ]; then
  echo "runsc_profile_skipped=true runtime=${RUNTIME_UNDER_TEST}" >&2
  jq -n --arg runtime "${RUNTIME_UNDER_TEST}" --arg nat "${NAT_BACKEND}" '{runtime:$runtime,natBackend:$nat,profileSkipped:true}'
  exit 0
fi

require_perf_ready
setup_external_probe

BPFNET_UPLINKS_CONFIG=""
if [ "${NAT_BACKEND}" = "ebpf" ]; then
  BPFNET_UPLINKS_CONFIG="$(bpfnet_ebpf_uplinks_config "${DEFAULT_UPLINK}")"
fi

cat > /tmp/axnoded-config.toml <<EOF
rootDir = "/var/lib/axnoded/root"
storeDir = "/var/lib/axnoded/store"

[plugin.network]
ip_range = "${AXNODED_IP_RANGE}"
nat_backend = "${NAT_BACKEND}"

[plugin.network.ebpf]
pin_path = "${BPFNET_PIN_PATH}"
map_size = ${BPFNET_MAP_SIZE}
snat_map_size = ${BPFNET_SNAT_MAP_SIZE}
snat_gc_interval = "${BPFNET_SNAT_GC_INTERVAL}"
snat_tcp_idle_timeout = "${BPFNET_SNAT_TCP_IDLE_TIMEOUT}"
snat_tcp_closing_timeout = "${BPFNET_SNAT_TCP_CLOSING_TIMEOUT}"
snat_datagram_idle_timeout = "${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT}"
local_out_compat = true
iptables_fallback = true
${BPFNET_UPLINKS_CONFIG}
[plugin.resource]
cgroup_cache_size = 4
interface_cache_size = 4
cgroup_root_name = "/sandbox"
max_instance_num = 8
recycle_policy = "destroy"

[plugin.runtime]
image_lib_dir = "/var/lib/axnoded/rootfs"
image_manager_enabled = false
volume_manager_socket = "${VOLUMED_SOCKET}"
ignore_cgroups = true
EOF

cat >> /tmp/axnoded-config.toml <<EOF

[plugin.runtime.runtimes.${RUNTIME_UNDER_TEST}]
binary = "${RUNTIME_BINARY}"
EOF

mkdir -p /var/lib/axnoded/root /var/lib/axnoded/store /var/lib/axnoded/rootfs /run/axnoded /tmp/runsc

AXNODED_PID=""
cleanup() {
  if [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    kill "${AXNODED_PID}" >/dev/null 2>&1 || true
    wait "${AXNODED_PID}" >/dev/null 2>&1 || true
  fi
  stop_node_runtime_volumed
  cleanup_external_probe
}
trap cleanup EXIT

start_node_runtime_volumed

"${AXNODED_BIN}" \
  -root /var/lib/axnoded \
  -config /tmp/axnoded-config.toml \
  -socket "${SOCKET_ADDRESS}" \
  -http-address 127.0.0.1:23001 \
  -log-level debug \
  -log-file /tmp/axnoded.log &

AXNODED_PID=$!

for _ in $(seq 1 30); do
  if [ -S "${SOCKET_ADDRESS}" ] && curl -fsS http://127.0.0.1:23001/readyz >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${SOCKET_ADDRESS}" ] || ! curl -fsS http://127.0.0.1:23001/readyz >/dev/null 2>&1; then
  echo "axnoded did not become ready in time" >&2
  echo "--- volumed log tail ---" >&2
  tail_node_runtime_volumed_log 120
  echo "--- axnoded log tail ---" >&2
  tail -n 120 /tmp/axnoded.log >&2 || true
  exit 1
fi

started_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
benchmark_json="/tmp/profile-benchmark.json"
perf_output="/tmp/profile-perf-output"

ROOT_DIR="${ROOT_DIR}" \
SOCKET_ADDRESS="${SOCKET_ADDRESS}" \
RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST}" \
NAT_BACKEND="${NAT_BACKEND}" \
BENCHMARK_REQUESTS="${BENCHMARK_REQUESTS}" \
BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY}" \
BENCHMARK_WARMUP_REQUESTS="${BENCHMARK_WARMUP_REQUESTS}" \
BENCHMARK_PATHS="${BENCHMARK_PATHS}" \
EBPF_INGRESS_PROBE_NETNS="${EBPF_INGRESS_PROBE_NETNS}" \
EBPF_INGRESS_PROBE_ADDR="${EBPF_INGRESS_PROBE_HOST_ADDR}" \
EBPF_INGRESS_PROBE_CLIENT_ADDR="${EBPF_INGRESS_PROBE_CLIENT_ADDR}" \
run_profile_with_retries "${perf_output}" "${benchmark_json}"

completed_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

if [ "${BENCHMARK_PROFILE_MODE}" = "record" ]; then
  jq -n \
    --arg runtime "${RUNTIME_UNDER_TEST}" \
    --arg nat "${NAT_BACKEND}" \
    --arg mode "${BENCHMARK_PROFILE_MODE}" \
    --arg events "${BENCHMARK_PROFILE_EVENTS:-${DEFAULT_PERF_RECORD_EVENT}}" \
    --arg started "${started_at}" \
    --arg completed "${completed_at}" \
    --slurpfile benchmark "${benchmark_json}" \
    --rawfile perfReport /tmp/profile-perf-report.txt \
    '{
      runtime: $runtime,
      natBackend: $nat,
      profileMode: $mode,
      profileEvents: $events,
      startedAt: $started,
      completedAt: $completed,
      benchmark: $benchmark[0],
      perfReport: $perfReport
    }'
else
  jq -n \
    --arg runtime "${RUNTIME_UNDER_TEST}" \
    --arg nat "${NAT_BACKEND}" \
    --arg mode "${BENCHMARK_PROFILE_MODE}" \
    --arg events "${BENCHMARK_PROFILE_EVENTS:-${DEFAULT_PERF_STAT_EVENTS}}" \
    --arg started "${started_at}" \
    --arg completed "${completed_at}" \
    --slurpfile benchmark "${benchmark_json}" \
    --rawfile perfStat "${perf_output}" \
    '{
      runtime: $runtime,
      natBackend: $nat,
      profileMode: $mode,
      profileEvents: $events,
      startedAt: $started,
      completedAt: $completed,
      benchmark: $benchmark[0],
      perfStat: $perfStat
    }'
fi
