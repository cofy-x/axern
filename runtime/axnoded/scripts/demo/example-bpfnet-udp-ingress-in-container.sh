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
VERIFY_UDP_BIN="${VERIFY_UDP_BIN:-/usr/local/bin/verify-udp}"
NAT_BACKEND="${NAT_BACKEND:-ebpf}"
DEFAULT_UPLINK="${DEFAULT_UPLINK:-$(ip route show default | awk '/default/ {print $5; exit}')}"
AXNODED_IP_RANGE="${AXNODED_IP_RANGE:-172.31.0.1/16}"
HELPER_DIR="${HELPER_DIR:-/usr/local/libexec/axnoded}"
VERIFY_ROOTFS="${VERIFY_ROOTFS:-/opt/sample-rootfs}"
VERIFY_STDOUT="${VERIFY_STDOUT:-/tmp/axnoded-example-udp.stdout}"
VERIFY_STDERR="${VERIFY_STDERR:-/tmp/axnoded-example-udp.stderr}"
LISTEN_PORT="${LISTEN_PORT:-15353}"
TARGET_PORT="${TARGET_PORT:-1053}"
setup_node_runtime_volume_defaults
ensure_bpf_fs "${NAT_BACKEND}"
setup_external_probe

BPFNET_UPLINKS_CONFIG=""
if [ "${NAT_BACKEND}" = "ebpf" ]; then
  BPFNET_UPLINKS_CONFIG="$(bpfnet_ebpf_uplinks_config "${DEFAULT_UPLINK}")"
fi

cat > /tmp/axnoded-example-config.toml <<EOF
rootDir = "/var/lib/axnoded/root"
storeDir = "/var/lib/axnoded/store"

[plugin.network]
ip_range = "${AXNODED_IP_RANGE}"
nat_backend = "${NAT_BACKEND}"

[plugin.network.ebpf]
pin_path = "/sys/fs/bpf/axern/bpfnet"
map_size = 16384
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
cgroup_enforcement = "disabled_dev"

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
  -config /tmp/axnoded-example-config.toml \
  -socket "${SOCKET_ADDRESS}" \
  -http-address 127.0.0.1:23001 \
  -log-level debug \
  -log-file /tmp/axnoded-example.log &

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
  tail -n 120 /tmp/axnoded-example.log >&2 || true
  exit 1
fi

udp_args=(
  -address "${SOCKET_ADDRESS}"
  -rootfs "${VERIFY_ROOTFS}"
  -runtime "${RUNTIME_UNDER_TEST}"
  -stdout "${VERIFY_STDOUT}"
  -stderr "${VERIFY_STDERR}"
  -listen-port "${LISTEN_PORT}"
  -target-port "${TARGET_PORT}"
  -nat-backend "${NAT_BACKEND}"
  -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
  -external-probe-address "${EBPF_INGRESS_PROBE_HOST_ADDR}"
  -helper-dir "${HELPER_DIR}"
)
if [ "${NAT_BACKEND}" = "ebpf" ]; then
  udp_args+=(-bpfnet-pin-path /sys/fs/bpf/axern/bpfnet)
fi

udp_output="$("${VERIFY_UDP_BIN}" "${udp_args[@]}" 2>&1)" || {
  printf '%s\n' "${udp_output}" >&2
  exit 1
}

printf '%s\n' "example_ok=true"
printf '%s\n' "example=bpfnet_udp_ingress"
printf '%s\n' "runtime=${RUNTIME_UNDER_TEST}"
printf '%s\n' "nat_backend=${NAT_BACKEND}"
printf '%s\n' "host_port=${LISTEN_PORT}"
printf '%s\n' "sandbox_port=${TARGET_PORT}"
printf '%s\n' "summary=external UDP hostPort ingress reached the sandbox responder"
