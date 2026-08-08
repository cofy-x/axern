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
filestore_mode = "loopback_dev"
filestore_dir = "/var/lib/axnoded/filestore"
filestore_loopback_image = "/var/lib/axnoded/filestore.xfs"
filestore_loopback_size_bytes = 1073741824
filestore_system_reserve_bytes = 67108864
writable_layer_default_limit_bytes = 268435456
EOF

cat >> /tmp/axnoded-config.toml <<EOF

[plugin.runtime.runtimes.${RUNTIME_UNDER_TEST}]
binary = "${RUNTIME_BINARY}"
EOF

mkdir -p \
  /var/lib/axnoded/root \
  /var/lib/axnoded/store \
  /var/lib/axnoded/rootfs \
  /var/lib/axnoded/filestore \
  /run/axnoded \
  /tmp/runsc

AXNODED_PID=""
cleanup() {
  if [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    kill "${AXNODED_PID}" >/dev/null 2>&1 || true
    wait "${AXNODED_PID}" >/dev/null 2>&1 || true
  fi
  stop_node_runtime_volumed
  if [ "${VERIFY_KEEP_EXTERNAL_PROBE:-false}" != "true" ]; then
    cleanup_external_probe
  fi
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

ROOT_DIR="${ROOT_DIR}" SOCKET_ADDRESS="${SOCKET_ADDRESS}" RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST}" \
  NAT_BACKEND="${NAT_BACKEND}" \
  bash "${ROOT_DIR}/scripts/verify/verify-generic-core.sh"
ROOT_DIR="${ROOT_DIR}" SOCKET_ADDRESS="${SOCKET_ADDRESS}" RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST}" \
  NAT_BACKEND="${NAT_BACKEND}" \
  EBPF_INGRESS_PROBE_NETNS="${EBPF_INGRESS_PROBE_NETNS}" \
  EBPF_INGRESS_PROBE_ADDR="${EBPF_INGRESS_PROBE_HOST_ADDR}" \
  EBPF_INGRESS_PROBE_CLIENT_ADDR="${EBPF_INGRESS_PROBE_CLIENT_ADDR}" \
  bash "${ROOT_DIR}/scripts/verify/verify-runsc-profile.sh"

echo "verify_in_container_ok=true"
