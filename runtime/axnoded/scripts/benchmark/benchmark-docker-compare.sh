#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/../lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

ensure_verify_image

tmpdir="$(mktemp -d)"
BENCHMARK_RUNS="${BENCHMARK_RUNS:-1}"
BENCHMARK_RUN_RETRIES="${BENCHMARK_RUN_RETRIES:-3}"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

is_known_runsc_flake() {
  local stderr_file="$1"
  grep -Eq 'runsc reported .* stopped but did not provide an exit status' "${stderr_file}"
}

run_backend() {
  local backend="$1"
  local target_dir="$2"
  mkdir -p "${target_dir}"
  local run=1
  while [ "${run}" -le "${BENCHMARK_RUNS}" ]; do
    local attempt=1
    local completed=0
    while [ "${attempt}" -le "${BENCHMARK_RUN_RETRIES}" ]; do
      local stdout_file="${target_dir}/run-${run}.json"
      local stderr_file="${target_dir}/run-${run}.attempt-${attempt}.stderr"
      echo "benchmark_compare_run backend=${backend} run=${run}/${BENCHMARK_RUNS} attempt=${attempt}/${BENCHMARK_RUN_RETRIES}" >&2
      if NAT_BACKEND="${backend}" run_verify_container \
        /bin/bash /workspace/scripts/benchmark/benchmark-in-container.sh \
        >"${stdout_file}" 2>"${stderr_file}"; then
        cat "${stderr_file}" >&2 || true
        rm -f "${stderr_file}"
        completed=1
        break
      fi

      cat "${stderr_file}" >&2 || true
      if is_known_runsc_flake "${stderr_file}" && [ "${attempt}" -lt "${BENCHMARK_RUN_RETRIES}" ]; then
        echo "benchmark_compare_retry backend=${backend} run=${run} reason=runsc_exit_status_unavailable" >&2
        rm -f "${stdout_file}" "${stderr_file}"
        attempt=$((attempt + 1))
        continue
      fi
      return 1
    done
    if [ "${completed}" -ne 1 ]; then
      echo "benchmark_compare_failed backend=${backend} run=${run}" >&2
      return 1
    fi
    run=$((run + 1))
  done
}

run_backend iptables "${tmpdir}/iptables"
run_backend ebpf "${tmpdir}/ebpf"

compare_json="${tmpdir}/compare.json"
GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12}" GOFLAGS="${GOFLAGS:--mod=readonly}" \
  go run ./cmd/natbench-compare \
    -iptables-dir "${tmpdir}/iptables" \
    -ebpf-dir "${tmpdir}/ebpf" \
    -expect-runs "${BENCHMARK_RUNS}" \
    >"${compare_json}"

jq -r '.comparison[] | "benchmark_compare path=\(.name) iptables_rps=\(.iptables.throughputRps) ebpf_rps=\(.ebpf.throughputRps) iptables_p95_ms=\(.iptables.p95Ms) ebpf_p95_ms=\(.ebpf.p95Ms) iptables_failures=\(.iptables.totalFailures // 0) ebpf_failures=\(.ebpf.totalFailures // 0) ebpf_failure_rate=\(.ebpf.failureRate // 0) ebpf_runs_with_failures=\(.ebpf.runsWithFailures // 0) ebpf_mappings_per_success=\(.ebpf.profile.snatMappingsPerSuccess // 0) ebpf_forward_reuse_ratio=\(.ebpf.profile.snatForwardReuseRatio // 0) ebpf_udp_same_port_ratio=\(.ebpf.profile.snatUdpSamePortRatio // 0) ebpf_udp_port_rewrite_ratio=\(.ebpf.profile.snatUdpPortRewriteRatio // 0) ebpf_udp_checksum_ratio=\(.ebpf.profile.snatUdpChecksumRatio // 0)"' "${compare_json}" >&2
cat "${compare_json}"
