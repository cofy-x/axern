#!/usr/bin/env bash
set -euo pipefail

RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST:-runsc}"
SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
VERIFY_NGINX_BIN="${VERIFY_NGINX_BIN:-/usr/local/bin/verify-nginx}"
VERIFY_EGRESS_BIN="${VERIFY_EGRESS_BIN:-/usr/local/bin/verify-egress}"
VERIFY_UDP_BIN="${VERIFY_UDP_BIN:-/usr/local/bin/verify-udp}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
BPFNET_PIN_PATH="${BPFNET_PIN_PATH:-/sys/fs/bpf/axern/bpfnet}"
BENCHMARK_REQUESTS="${BENCHMARK_REQUESTS:-200}"
BENCHMARK_CONCURRENCY="${BENCHMARK_CONCURRENCY:-16}"
BENCHMARK_WARMUP_REQUESTS="${BENCHMARK_WARMUP_REQUESTS:-64}"
BENCHMARK_MULTI_CLIENT_COUNT="${BENCHMARK_MULTI_CLIENT_COUNT:-4}"
BENCHMARK_SNAT_POST_GC_WAIT="${BENCHMARK_SNAT_POST_GC_WAIT:-12s}"
BENCHMARK_PATHS="${BENCHMARK_PATHS:-}"
BENCHMARK_PATHS="${BENCHMARK_PATHS// /}"
EBPF_INGRESS_PROBE_NETNS="${EBPF_INGRESS_PROBE_NETNS:-}"
EBPF_INGRESS_PROBE_ADDR="${EBPF_INGRESS_PROBE_ADDR:-}"
EBPF_INGRESS_PROBE_CLIENT_ADDR="${EBPF_INGRESS_PROBE_CLIENT_ADDR:-}"
VERIFY_SKIP_LOCALHOST="${VERIFY_SKIP_LOCALHOST:-false}"

if [ "${RUNTIME_UNDER_TEST}" != "runsc" ]; then
  echo "runsc_benchmark_skipped=true runtime=${RUNTIME_UNDER_TEST}" >&2
  jq -n --arg runtime "${RUNTIME_UNDER_TEST}" --arg nat "${NAT_BACKEND}" '{runtime:$runtime,natBackend:$nat,paths:[]}'
  exit 0
fi

started_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

path_enabled() {
  local path="$1"
  if [ -z "${BENCHMARK_PATHS}" ]; then
    return 0
  fi
  case ",${BENCHMARK_PATHS}," in
    *,"${path}",*) return 0 ;;
    *) return 1 ;;
  esac
}

egress_transport_for_path() {
  case "$1" in
    egress_tcp_short) printf '%s\n' tcp-short ;;
    egress_tcp_short_multi_client) printf '%s\n' tcp-short ;;
    egress_tcp_reuse) printf '%s\n' tcp-reuse ;;
    egress_tcp_pool) printf '%s\n' tcp-pool ;;
    egress_udp) printf '%s\n' udp ;;
    egress_udp_connected) printf '%s\n' udp-connected ;;
  esac
}

egress_client_count_for_path() {
  case "$1" in
    egress_tcp_short_multi_client) printf '%s\n' "${BENCHMARK_MULTI_CLIENT_COUNT}" ;;
    *) printf '%s\n' "1" ;;
  esac
}

egress_path_suffix() {
  printf '%s\n' "$1" | tr '-' '_'
}

write_empty_report() {
  local path="$1"
  jq -n \
    --arg runtime "${RUNTIME_UNDER_TEST}" \
    --arg nat "${NAT_BACKEND}" \
    '{runtime:$runtime,natBackend:$nat,paths:[]}' >"${path}"
}

common_args=(
  -address "${SOCKET_ADDRESS}"
  -runtime "${RUNTIME_UNDER_TEST}"
  -nat-backend "${NAT_BACKEND}"
  -benchmark-requests "${BENCHMARK_REQUESTS}"
  -benchmark-concurrency "${BENCHMARK_CONCURRENCY}"
  -benchmark-warmup-requests "${BENCHMARK_WARMUP_REQUESTS}"
)

if path_enabled external_tcp_ingress; then
  nginx_args=(
    "${common_args[@]}"
    -rootfs /opt/nginx-rootfs
    -stdout /tmp/axnoded-nginx-benchmark.stdout
    -stderr /tmp/axnoded-nginx-benchmark.stderr
    -listen-port 18080
    -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
    -external-probe-address "${EBPF_INGRESS_PROBE_ADDR}"
    -benchmark-output /tmp/benchmark-nginx.json
  )
  if [ "${NAT_BACKEND}" = "ebpf" ]; then
    nginx_args+=(-bpfnet-pin-path "${BPFNET_PIN_PATH}")
  fi
  if [ "${VERIFY_SKIP_LOCALHOST}" = "true" ]; then
    nginx_args+=(-skip-localhost-check)
  fi
  "${VERIFY_NGINX_BIN}" "${nginx_args[@]}" >&2
else
  write_empty_report /tmp/benchmark-nginx.json
fi

if path_enabled external_udp_ingress; then
  udp_args=(
    "${common_args[@]}"
    -rootfs /opt/sample-rootfs
    -stdout /tmp/axnoded-udp-benchmark.stdout
    -stderr /tmp/axnoded-udp-benchmark.stderr
    -listen-port 15353
    -target-port 1053
    -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
    -external-probe-address "${EBPF_INGRESS_PROBE_ADDR}"
    -benchmark-output /tmp/benchmark-udp.json
  )
  if [ "${NAT_BACKEND}" = "ebpf" ]; then
    udp_args+=(-bpfnet-pin-path "${BPFNET_PIN_PATH}")
  fi
  "${VERIFY_UDP_BIN}" "${udp_args[@]}" >&2
else
  write_empty_report /tmp/benchmark-udp.json
fi

egress_specs=()
seen_egress_paths=","
if [ -n "${BENCHMARK_PATHS}" ]; then
  IFS=',' read -r -a requested_paths <<<"${BENCHMARK_PATHS}"
  for requested_path in "${requested_paths[@]}"; do
    transport="$(egress_transport_for_path "${requested_path}")"
    if [ -n "${transport}" ] && [[ "${seen_egress_paths}" != *",${requested_path},"* ]]; then
      egress_specs+=("${requested_path}|${transport}|$(egress_client_count_for_path "${requested_path}")")
      seen_egress_paths="${seen_egress_paths}${requested_path},"
    fi
  done
else
  egress_specs=(
    "egress_udp|udp|1"
    "egress_udp_connected|udp-connected|1"
    "egress_tcp_short|tcp-short|1"
  )
fi
if [ "${#egress_specs[@]}" -gt 0 ]; then
  egress_reports=()
  for egress_spec in "${egress_specs[@]}"; do
    IFS='|' read -r egress_path transport client_count <<<"${egress_spec}"
    suffix="$(egress_path_suffix "${egress_path}")"
    report="/tmp/benchmark-egress-${suffix}.json"
    egress_reports+=("${report}")
    egress_args=(
      "${common_args[@]}"
      -rootfs /opt/sample-rootfs
      -stdout "/tmp/axnoded-egress-benchmark-${suffix}.stdout"
      -stderr "/tmp/axnoded-egress-benchmark-${suffix}.stderr"
      -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
      -external-probe-address "${EBPF_INGRESS_PROBE_CLIENT_ADDR}"
      -expected-source-ip "${EBPF_INGRESS_PROBE_ADDR}"
      -benchmark-transports "${transport}"
      -benchmark-client-count "${client_count}"
      -benchmark-snat-post-gc-wait "${BENCHMARK_SNAT_POST_GC_WAIT}"
      -benchmark-output "${report}"
    )
    if [ "${NAT_BACKEND}" = "ebpf" ]; then
      egress_args+=(-bpfnet-pin-path "${BPFNET_PIN_PATH}")
    fi
    "${VERIFY_EGRESS_BIN}" "${egress_args[@]}" >&2
  done
  jq -s '{
    runtime: .[0].runtime,
    natBackend: .[0].natBackend,
    startup: .[-1].startup,
    locality: .[-1].locality,
    startedAt: .[0].startedAt,
    completedAt: .[-1].completedAt,
    paths: (map(.paths // []) | add)
  }' "${egress_reports[@]}" > /tmp/benchmark-egress.json
else
  write_empty_report /tmp/benchmark-egress.json
fi

completed_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

jq -n \
  --arg runtime "${RUNTIME_UNDER_TEST}" \
  --arg nat "${NAT_BACKEND}" \
  --arg started "${started_at}" \
  --arg completed "${completed_at}" \
  --slurpfile nginx /tmp/benchmark-nginx.json \
  --slurpfile udp /tmp/benchmark-udp.json \
  --slurpfile egress /tmp/benchmark-egress.json \
  '{
    runtime: $runtime,
    natBackend: $nat,
    startedAt: $started,
    completedAt: $completed,
    paths: (($nginx[0].paths // []) + ($udp[0].paths // []) + ($egress[0].paths // []))
  }'
