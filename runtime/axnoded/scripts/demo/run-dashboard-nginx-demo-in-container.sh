#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/lib/node-runtime-services.sh"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
VOLUMED_LOG="${VOLUMED_LOG:-/tmp/volumed-dashboard.log}"
setup_node_runtime_volume_defaults

ensure_bpf_fs() {
  if [ "${NAT_BACKEND}" != "ebpf" ]; then
    return 0
  fi
  mkdir -p /sys/fs/bpf
  if ! mountpoint -q /sys/fs/bpf; then
    mount -t bpf bpffs /sys/fs/bpf
  fi
}

ensure_bpf_fs

cat > /tmp/axnoded-dashboard-config.toml <<EOF
rootDir = "/var/lib/axnoded/root"
storeDir = "/var/lib/axnoded/store"

[plugin.network]
ip_range = "172.17.0.1/16"
nat_backend = "${NAT_BACKEND}"

[plugin.network.ebpf]
pin_path = "/sys/fs/bpf/axern/bpfnet"
map_size = 16384
local_out_compat = true
iptables_fallback = true

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

[plugin.runtime.runtimes.runsc]
binary = "/usr/local/bin/runsc"

[plugin.runtime.runtimes.runsc.options]

[plugin.runtime.runtimes.runc]
binary = "/usr/bin/runc"
EOF

mkdir -p /var/lib/axnoded/root /var/lib/axnoded/store /var/lib/axnoded/rootfs /run/axnoded /tmp/runsc

AXNODED_PID=""
cleanup() {
  if [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    kill "${AXNODED_PID}" >/dev/null 2>&1 || true
    wait "${AXNODED_PID}" >/dev/null 2>&1 || true
  fi
  stop_node_runtime_volumed
}
trap cleanup EXIT

start_node_runtime_volumed

/usr/local/bin/axnoded \
  -root /var/lib/axnoded \
  -config /tmp/axnoded-dashboard-config.toml \
  -socket /run/axnoded/axnoded.sock \
  -http-address 0.0.0.0:23001 \
  -log-level debug \
  -log-file /tmp/axnoded-dashboard.log &

AXNODED_PID=$!

for _ in $(seq 1 30); do
  if curl -fsS --connect-timeout 1 --max-time 2 \
    http://127.0.0.1:23001/demo/nginx >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

wait "${AXNODED_PID}"
