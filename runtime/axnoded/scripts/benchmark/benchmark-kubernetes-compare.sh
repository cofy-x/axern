#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
AXERN_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
CALLER_DIR="$(pwd)"
cd "${ROOT_DIR}"

KUBECTL="${KUBECTL:-kubectl}"
KUBE_NAMESPACE="${KUBE_NAMESPACE:-axern-system}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
KUBECONFIG="${KUBECONFIG:-}"
BENCHMARK_IMAGE="${BENCHMARK_IMAGE:-${IMAGE_TAG:-axnoded-verify:latest}}"
BENCHMARK_IMAGE_PULL_POLICY="${BENCHMARK_IMAGE_PULL_POLICY:-IfNotPresent}"
BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS:-}"
SOURCE_DAEMONSET="${SOURCE_DAEMONSET:-node-all-in-one}"
JOB_PREFIX="${JOB_PREFIX:-axern-bpfnet-bench}"
JOB_TIMEOUT="${JOB_TIMEOUT:-20m}"
JOB_ACTIVE_DEADLINE_SECONDS="${JOB_ACTIVE_DEADLINE_SECONDS:-1800}"
JOB_TTL_SECONDS="${JOB_TTL_SECONDS:-3600}"
BENCHMARK_RUNS="${BENCHMARK_RUNS:-1}"
BENCHMARK_BACKENDS="${BENCHMARK_BACKENDS:-iptables,ebpf}"
BENCHMARK_REQUESTS="${BENCHMARK_REQUESTS:-1000}"
BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY:-16}"
BENCHMARK_WARMUP_REQUESTS="${BENCHMARK_WARMUP_REQUESTS:-64}"
BENCHMARK_MULTI_CLIENT_COUNT="${BENCHMARK_MULTI_CLIENT_COUNT:-4}"
BENCHMARK_SNAT_POST_GC_WAIT="${BENCHMARK_SNAT_POST_GC_WAIT:-12s}"
BENCHMARK_PATHS="${BENCHMARK_PATHS:-external_tcp_ingress,external_udp_ingress,egress_udp,egress_udp_connected,egress_tcp_short}"
RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST:-runsc}"
RUNTIME_BINARY="${RUNTIME_BINARY:-/usr/local/bin/runsc}"
VERIFY_SKIP_LOCALHOST="${VERIFY_SKIP_LOCALHOST:-false}"
BPFNET_PIN_PATH="${BPFNET_PIN_PATH:-/sys/fs/bpf/axern/bpfnet}"
BPFNET_MAP_SIZE="${BPFNET_MAP_SIZE:-16384}"
BPFNET_SNAT_MAP_SIZE="${BPFNET_SNAT_MAP_SIZE:-262144}"
BPFNET_SNAT_GC_INTERVAL="${BPFNET_SNAT_GC_INTERVAL:-1s}"
BPFNET_SNAT_TCP_IDLE_TIMEOUT="${BPFNET_SNAT_TCP_IDLE_TIMEOUT:-5m}"
BPFNET_SNAT_TCP_CLOSING_TIMEOUT="${BPFNET_SNAT_TCP_CLOSING_TIMEOUT:-2s}"
BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT="${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT:-10s}"
BENCHMARK_NODE_NAME="${BENCHMARK_NODE_NAME:-}"
BENCHMARK_CPU_REQUEST="${BENCHMARK_CPU_REQUEST:-}"
BENCHMARK_MEMORY_REQUEST="${BENCHMARK_MEMORY_REQUEST:-}"
BENCHMARK_CPU_LIMIT="${BENCHMARK_CPU_LIMIT:-}"
BENCHMARK_MEMORY_LIMIT="${BENCHMARK_MEMORY_LIMIT:-}"
OUTPUT_DIR="${OUTPUT_DIR:-${AXERN_ROOT}/work/axnoded-bpfnet-benchmark/$(date -u +%Y%m%dT%H%M%SZ)}"

abs_from_caller() {
  local path="$1"
  if [[ -z "${path}" || "${path}" == /* ]]; then
    printf '%s\n' "${path}"
  else
    printf '%s/%s\n' "${CALLER_DIR}" "${path}"
  fi
}

if [[ -n "${KUBECONFIG}" ]]; then
  KUBECONFIG="$(abs_from_caller "${KUBECONFIG}")"
fi
OUTPUT_DIR="$(abs_from_caller "${OUTPUT_DIR}")"

usage() {
  cat >&2 <<EOF
Usage: BENCHMARK_IMAGE=<image> [KUBECONFIG=<path>] $0

Runs Axern NAT benchmark Jobs in Kubernetes for iptables and ebpf, then writes
per-run JSON reports plus compare.json.

Important env:
  KUBE_NAMESPACE                 default: axern-system
  BENCHMARK_IMAGE                default: axnoded-verify:latest
  BENCHMARK_IMAGE_PULL_SECRETS   comma-separated secret names; defaults to source DaemonSet secrets
  BENCHMARK_RUNS                 default: 1
  BENCHMARK_REQUESTS             default: 1000
  BENCHMARK_CONCURRENCY          default: 16
  BENCHMARK_MULTI_CLIENT_COUNT   default: 4 for egress_tcp_short_multi_client
  BENCHMARK_SNAT_POST_GC_WAIT    default: 12s, egress SNAT post-GC snapshot grace
  BENCHMARK_PATHS                comma-separated benchmark paths
  BPFNET_SNAT_GC_INTERVAL        default: 1s for stale egress SNAT mapping cleanup
  BPFNET_SNAT_TCP_CLOSING_TIMEOUT default: 2s for TCP FIN/RST flow cleanup
  BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT default: 10s for UDP/ICMP mapping cleanup
  BENCHMARK_NODE_NAME            optional nodeName pin for the benchmark Job
  BENCHMARK_CPU_REQUEST          optional benchmark container CPU request
  BENCHMARK_MEMORY_REQUEST       optional benchmark container memory request
  BENCHMARK_CPU_LIMIT            optional benchmark container CPU limit
  BENCHMARK_MEMORY_LIMIT         optional benchmark container memory limit
  JOB_TIMEOUT                    default: 20m
  JOB_ACTIVE_DEADLINE_SECONDS    default: 1800
  OUTPUT_DIR                     default: work/axnoded-bpfnet-benchmark/<timestamp>
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

kube_args=()
if [[ -n "${KUBECONFIG}" ]]; then
  kube_args+=(--kubeconfig "${KUBECONFIG}")
fi
if [[ -n "${KUBE_CONTEXT}" ]]; then
  kube_args+=(--context "${KUBE_CONTEXT}")
fi

kube() {
  "${KUBECTL}" "${kube_args[@]}" "$@"
}

trim_space() {
  local value=$1
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s\n' "${value}"
}

render_image_pull_secrets() {
  local secrets="$1"
  local secret
  if [[ -z "${secrets}" ]]; then
    return 0
  fi
  printf '      imagePullSecrets:\n'
  while IFS= read -r secret; do
    secret="$(trim_space "${secret}")"
    if [[ -n "${secret}" ]]; then
      printf '        - name: %s\n' "${secret}"
    fi
  done < <(printf '%s\n' "${secrets}" | tr ',' '\n')
}

render_node_name() {
  if [[ -n "${BENCHMARK_NODE_NAME}" ]]; then
    printf '      nodeName: %s\n' "${BENCHMARK_NODE_NAME}"
  fi
}

render_resources() {
  if [[ -z "${BENCHMARK_CPU_REQUEST}${BENCHMARK_MEMORY_REQUEST}${BENCHMARK_CPU_LIMIT}${BENCHMARK_MEMORY_LIMIT}" ]]; then
    return 0
  fi
  printf '          resources:\n'
  if [[ -n "${BENCHMARK_CPU_REQUEST}${BENCHMARK_MEMORY_REQUEST}" ]]; then
    printf '            requests:\n'
    if [[ -n "${BENCHMARK_CPU_REQUEST}" ]]; then
      printf '              cpu: "%s"\n' "${BENCHMARK_CPU_REQUEST}"
    fi
    if [[ -n "${BENCHMARK_MEMORY_REQUEST}" ]]; then
      printf '              memory: "%s"\n' "${BENCHMARK_MEMORY_REQUEST}"
    fi
  fi
  if [[ -n "${BENCHMARK_CPU_LIMIT}${BENCHMARK_MEMORY_LIMIT}" ]]; then
    printf '            limits:\n'
    if [[ -n "${BENCHMARK_CPU_LIMIT}" ]]; then
      printf '              cpu: "%s"\n' "${BENCHMARK_CPU_LIMIT}"
    fi
    if [[ -n "${BENCHMARK_MEMORY_LIMIT}" ]]; then
      printf '              memory: "%s"\n' "${BENCHMARK_MEMORY_LIMIT}"
    fi
  fi
}

duration_seconds() {
  local value="$1"
  local number
  case "${value}" in
    *s)
      number="${value%s}"
      printf '%s\n' "${number}"
      ;;
    *m)
      number="${value%m}"
      printf '%s\n' "$((number * 60))"
      ;;
    *h)
      number="${value%h}"
      printf '%s\n' "$((number * 3600))"
      ;;
    *) printf '%s\n' "${value}" ;;
  esac
}

job_yaml() {
  local backend="$1"
  local run="$2"
  local job="$3"
  cat <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job}
  namespace: ${KUBE_NAMESPACE}
  labels:
    app.kubernetes.io/name: axern-bpfnet-benchmark
    axern.dev/benchmark-backend: ${backend}
    axern.dev/benchmark-run: "${run}"
spec:
  backoffLimit: 0
  activeDeadlineSeconds: ${JOB_ACTIVE_DEADLINE_SECONDS}
  ttlSecondsAfterFinished: ${JOB_TTL_SECONDS}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: axern-bpfnet-benchmark
        axern.dev/benchmark-backend: ${backend}
        axern.dev/benchmark-run: "${run}"
    spec:
      restartPolicy: Never
$(render_node_name)
$(render_image_pull_secrets "${BENCHMARK_IMAGE_PULL_SECRETS}")
      containers:
        - name: benchmark
          image: ${BENCHMARK_IMAGE}
          imagePullPolicy: ${BENCHMARK_IMAGE_PULL_POLICY}
          securityContext:
            privileged: true
          env:
            - name: NAT_BACKEND
              value: ${backend}
            - name: RUNTIME_UNDER_TEST
              value: ${RUNTIME_UNDER_TEST}
            - name: RUNTIME_BINARY
              value: ${RUNTIME_BINARY}
            - name: BENCHMARK_REQUESTS
              value: "${BENCHMARK_REQUESTS}"
            - name: BENCHMARK_CONCURRENCY
              value: "${BENCHMARK_CONCURRENCY}"
            - name: BENCHMARK_WARMUP_REQUESTS
              value: "${BENCHMARK_WARMUP_REQUESTS}"
            - name: BENCHMARK_MULTI_CLIENT_COUNT
              value: "${BENCHMARK_MULTI_CLIENT_COUNT}"
            - name: BENCHMARK_SNAT_POST_GC_WAIT
              value: "${BENCHMARK_SNAT_POST_GC_WAIT}"
            - name: BENCHMARK_PATHS
              value: ${BENCHMARK_PATHS}
            - name: VERIFY_SKIP_LOCALHOST
              value: "${VERIFY_SKIP_LOCALHOST}"
            - name: BPFNET_PIN_PATH
              value: "${BPFNET_PIN_PATH}"
            - name: BPFNET_MAP_SIZE
              value: "${BPFNET_MAP_SIZE}"
            - name: BPFNET_SNAT_MAP_SIZE
              value: "${BPFNET_SNAT_MAP_SIZE}"
            - name: BPFNET_SNAT_GC_INTERVAL
              value: "${BPFNET_SNAT_GC_INTERVAL}"
            - name: BPFNET_SNAT_TCP_IDLE_TIMEOUT
              value: "${BPFNET_SNAT_TCP_IDLE_TIMEOUT}"
            - name: BPFNET_SNAT_TCP_CLOSING_TIMEOUT
              value: "${BPFNET_SNAT_TCP_CLOSING_TIMEOUT}"
            - name: BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT
              value: "${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT}"
$(render_resources)
          command:
            - /bin/bash
            - -lc
          args:
            - |
              set +e
              mkdir -p /tmp/axern-bpfnet-benchmark
              /bin/bash /workspace/scripts/benchmark/benchmark-in-container.sh \
                >/tmp/axern-bpfnet-benchmark/report.json \
                2>/tmp/axern-bpfnet-benchmark/stderr.log
              rc=\$?
              echo "__AXERN_BPFNET_BENCHMARK_REPORT_BEGIN__"
              if [ -s /tmp/axern-bpfnet-benchmark/report.json ]; then
                cat /tmp/axern-bpfnet-benchmark/report.json
              fi
              echo "__AXERN_BPFNET_BENCHMARK_REPORT_END__"
              echo "__AXERN_BPFNET_BENCHMARK_STDERR_BEGIN__"
              if [ -s /tmp/axern-bpfnet-benchmark/stderr.log ]; then
                cat /tmp/axern-bpfnet-benchmark/stderr.log
              fi
              echo "__AXERN_BPFNET_BENCHMARK_STDERR_END__"
              if [ "\${rc}" -ne 0 ]; then
                echo "__AXERN_BPFNET_BENCHMARK_DIAGNOSTICS_BEGIN__"
                if command -v bpfnetctl >/dev/null 2>&1; then
                  bpfnetctl status --json || true
                  bpfnetctl check --json || true
                fi
                if [ -s /var/run/axern/bpfnet/dataplane_state.json ]; then
                  cat /var/run/axern/bpfnet/dataplane_state.json
                fi
                if [ -s /tmp/axnoded.log ]; then
                  tail -n 200 /tmp/axnoded.log
                fi
                echo "__AXERN_BPFNET_BENCHMARK_DIAGNOSTICS_END__"
              fi
              exit "\${rc}"
EOF
}

wait_for_job() {
  local job="$1"
  local timeout="$2"
  local timeout_seconds
  timeout_seconds="$(duration_seconds "${timeout}")"
  local deadline=$((SECONDS + timeout_seconds))
  local complete failed

  while (( SECONDS < deadline )); do
    complete="$(kube -n "${KUBE_NAMESPACE}" get "job/${job}" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' 2>/dev/null || true)"
    if [[ "${complete}" == "True" ]]; then
      return 0
    fi
    failed="$(kube -n "${KUBE_NAMESPACE}" get "job/${job}" -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null || true)"
    if [[ "${failed}" == "True" ]]; then
      return 1
    fi
    sleep 5
  done

  echo "timed out waiting for job/${job} after ${timeout}" >&2
  return 1
}

extract_report() {
  local log_file="$1"
  local report_file="$2"
  awk '
    /__AXERN_BPFNET_BENCHMARK_REPORT_BEGIN__/ { in_report=1; next }
    /__AXERN_BPFNET_BENCHMARK_REPORT_END__/ { in_report=0; next }
    in_report { print }
  ' "${log_file}" >"${report_file}"
}

run_job() {
  local backend="$1"
  local run="$2"
  local target_dir="$3"
  local job="${JOB_PREFIX}-${backend}-${run}"
  local log_file="${target_dir}/run-${run}.log"
  local report_file="${target_dir}/run-${run}.json"

  mkdir -p "${target_dir}"
  kube -n "${KUBE_NAMESPACE}" delete job "${job}" --ignore-not-found --wait=true >/dev/null
  job_yaml "${backend}" "${run}" "${job}" | kube apply -f - >/dev/null
  echo "benchmark_kubernetes_job_started backend=${backend} run=${run} job=${job}" >&2

  if ! wait_for_job "${job}" "${JOB_TIMEOUT}"; then
    kube -n "${KUBE_NAMESPACE}" logs "job/${job}" --all-containers=true >"${log_file}" 2>&1 || true
    cat "${log_file}" >&2 || true
    kube -n "${KUBE_NAMESPACE}" describe "job/${job}" >&2 || true
    echo "benchmark Kubernetes job failed or timed out: ${job}" >&2
    return 1
  fi

  kube -n "${KUBE_NAMESPACE}" logs "job/${job}" --all-containers=true >"${log_file}"
  extract_report "${log_file}" "${report_file}"
  if ! jq empty "${report_file}" >/dev/null; then
    echo "benchmark report is not valid JSON: ${report_file}" >&2
    cat "${log_file}" >&2
    return 1
  fi
  echo "benchmark_kubernetes_report=${report_file}" >&2
}

require_cmd "${KUBECTL}"
require_cmd jq
require_cmd go

if [[ -z "${BENCHMARK_IMAGE_PULL_SECRETS}" ]]; then
  BENCHMARK_IMAGE_PULL_SECRETS="$(kube -n "${KUBE_NAMESPACE}" get ds "${SOURCE_DAEMONSET}" -o jsonpath='{range .spec.template.spec.imagePullSecrets[*]}{.name}{","}{end}' 2>/dev/null || true)"
  BENCHMARK_IMAGE_PULL_SECRETS="${BENCHMARK_IMAGE_PULL_SECRETS%,}"
fi

mkdir -p "${OUTPUT_DIR}"
printf 'benchmark_image=%s\n' "${BENCHMARK_IMAGE}" >&2
printf 'benchmark_output_dir=%s\n' "${OUTPUT_DIR}" >&2

IFS=',' read -r -a backends <<<"${BENCHMARK_BACKENDS}"
for backend in "${backends[@]}"; do
  backend="$(trim_space "${backend}")"
  if [[ -z "${backend}" ]]; then
    continue
  fi
  for run in $(seq 1 "${BENCHMARK_RUNS}"); do
    run_job "${backend}" "${run}" "${OUTPUT_DIR}/${backend}"
  done
done

if [[ -d "${OUTPUT_DIR}/iptables" && -d "${OUTPUT_DIR}/ebpf" ]]; then
  go run ./cmd/natbench-compare \
    -iptables-dir "${OUTPUT_DIR}/iptables" \
    -ebpf-dir "${OUTPUT_DIR}/ebpf" \
    -expect-runs "${BENCHMARK_RUNS}" \
    >"${OUTPUT_DIR}/compare.json"
  jq -r '
    .comparison[] |
    "benchmark_compare" +
    " path=\(.name)" +
    " iptables_rps=\(.iptables.throughputRps)" +
    " ebpf_rps=\(.ebpf.throughputRps)" +
    " iptables_p95_ms=\(.iptables.p95Ms)" +
    " ebpf_p95_ms=\(.ebpf.p95Ms)" +
    " iptables_failures=\(.iptables.totalFailures // 0)" +
    " ebpf_failures=\(.ebpf.totalFailures // 0)" +
    " ebpf_failure_rate=\(.ebpf.failureRate // 0)" +
    " ebpf_runs_with_failures=\(.ebpf.runsWithFailures // 0)" +
    " iptables_client_peak_entries=\(.iptables.clientPeak.tcpTable.entries // 0)" +
    " ebpf_client_peak_entries=\(.ebpf.clientPeak.tcpTable.entries // 0)" +
    " iptables_client_peak_established=\(.iptables.clientPeak.tcpTable.established // 0)" +
    " ebpf_client_peak_established=\(.ebpf.clientPeak.tcpTable.established // 0)" +
    " iptables_client_peak_tw=\(.iptables.clientPeak.tcpTimeWaitCount // 0)" +
    " ebpf_client_peak_tw=\(.ebpf.clientPeak.tcpTimeWaitCount // 0)" +
    " iptables_client_samples=\(.iptables.clientSamples // 0)" +
    " ebpf_client_samples=\(.ebpf.clientSamples // 0)" +
    " ebpf_snat_samples=\(.ebpf.snatMapSamples // 0)" +
    " ebpf_mappings_per_success=\(.ebpf.profile.snatMappingsPerSuccess // 0)" +
    " ebpf_forward_reuse_ratio=\(.ebpf.profile.snatForwardReuseRatio // 0)" +
    " ebpf_snat_fwd_peak=\(.ebpf.snatMapPeak.fwdEntries // 0)" +
    " ebpf_snat_fwd_peak_full_closing=\(.ebpf.snatMapPeak.fwdFullClosingEntries // 0)" +
    " ebpf_snat_ports_peak=\(.ebpf.snatMapPeak.translatedPortsUsed // 0)" +
    " ebpf_snat_fwd_after=\(.ebpf.snatMapAfter.fwdEntries // 0)" +
    " ebpf_snat_fwd_after_full_closing=\(.ebpf.snatMapAfter.fwdFullClosingEntries // 0)" +
    " ebpf_snat_fwd_post_gc=\(.ebpf.snatMapPostGc.fwdEntries // 0)" +
    " ebpf_snat_fwd_gc_released=\(.ebpf.snatMapGcReleased.fwdEntries // 0)" +
    " ebpf_full_close_reclaims=\(.ebpf.kernelDelta.snatFullCloseReclaims // 0)" +
    " ebpf_full_close_marks=\(.ebpf.kernelDelta.snatFullCloseMarks // 0)" +
    " ebpf_full_close_deletes=\(.ebpf.kernelDelta.snatTcpFullCloseDeletes // 0)" +
    " ebpf_full_close_deletes_fwd=\(.ebpf.kernelDelta.snatTcpFullCloseDeletesFwd // 0)" +
    " ebpf_full_close_deletes_rev=\(.ebpf.kernelDelta.snatTcpFullCloseDeletesRev // 0)" +
    " ebpf_alloc_exhausted=\(.ebpf.kernelDelta.snatAllocExhausted // 0)" +
    " ebpf_tcp_non_syn_misses=\(.ebpf.kernelDelta.snatTcpNonSynMisses // 0)" +
    " ebpf_tcp_non_syn_miss_fins=\(.ebpf.kernelDelta.snatTcpNonSynMissFins // 0)" +
    " ebpf_tcp_non_syn_miss_rsts=\(.ebpf.kernelDelta.snatTcpNonSynMissRsts // 0)" +
    " ebpf_tcp_non_syn_miss_acks=\(.ebpf.kernelDelta.snatTcpNonSynMissAcks // 0)" +
    " ebpf_tcp_non_syn_miss_other=\(.ebpf.kernelDelta.snatTcpNonSynMissOther // 0)" +
    " ebpf_tcp_non_syn_miss_fwd_lookups=\(.ebpf.kernelDelta.snatTcpNonSynMissFwdLookups // 0)" +
    " ebpf_tcp_non_syn_miss_fwd_host_mismatches=\(.ebpf.kernelDelta.snatTcpNonSynMissFwdHostMismatches // 0)" +
    " ebpf_tcp_reverse_misses=\(.ebpf.kernelDelta.snatTcpReverseMisses // 0)" +
    " ebpf_tcp_reverse_miss_syn_acks=\(.ebpf.kernelDelta.snatTcpReverseMissSynAcks // 0)" +
    " ebpf_tcp_reverse_miss_fins=\(.ebpf.kernelDelta.snatTcpReverseMissFins // 0)" +
    " ebpf_tcp_reverse_miss_rsts=\(.ebpf.kernelDelta.snatTcpReverseMissRsts // 0)" +
    " ebpf_tcp_reverse_miss_acks=\(.ebpf.kernelDelta.snatTcpReverseMissAcks // 0)" +
    " ebpf_tcp_reverse_miss_other=\(.ebpf.kernelDelta.snatTcpReverseMissOther // 0)"
  ' "${OUTPUT_DIR}/compare.json" >&2
  cat "${OUTPUT_DIR}/compare.json"
fi
