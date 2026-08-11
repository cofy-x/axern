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
setup_node_runtime_volume_defaults
ensure_bpf_fs "${NAT_BACKEND}"

if [ "${RUNTIME_UNDER_TEST}" = "runsc" ]; then
  setup_external_probe
fi

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
cgroup_root_name = "sandbox"
max_instance_num = 8

[plugin.runtime]
image_lib_dir = "/var/lib/axnoded/rootfs"
image_manager_enabled = false
volume_manager_socket = "${VOLUMED_SOCKET}"
cgroup_enforcement = "disabled_dev"
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

ROOT_DIR="${ROOT_DIR}" \
SOCKET_ADDRESS="${SOCKET_ADDRESS}" \
RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST}" \
NAT_BACKEND="${NAT_BACKEND}" \
EBPF_INGRESS_PROBE_NETNS="${EBPF_INGRESS_PROBE_NETNS}" \
EBPF_INGRESS_PROBE_ADDR="${EBPF_INGRESS_PROBE_HOST_ADDR}" \
EBPF_INGRESS_PROBE_CLIENT_ADDR="${EBPF_INGRESS_PROBE_CLIENT_ADDR}" \
bash "${ROOT_DIR}/scripts/benchmark/benchmark-runsc-profile.sh"
