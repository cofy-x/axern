#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/lib/node-runtime-services.sh"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
VOLUMED_LOG="${VOLUMED_LOG:-/tmp/volumed-dashboard.log}"
VERIFY_NGINX_ROOTFS_IMAGE="${VERIFY_NGINX_ROOTFS_IMAGE:-/var/lib/axnoded/verify-dashboard-nginx-rootfs.ext4}"
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
filestore_mode = "loopback_dev"
filestore_dir = "/var/lib/axnoded/filestore"
filestore_loopback_image = "/var/lib/axnoded/filestore.xfs"
filestore_loopback_size_bytes = 1073741824
filestore_system_reserve_bytes = 67108864
writable_layer_default_limit_bytes = 268435456

[plugin.runtime.runtimes.runsc]
binary = "/usr/local/bin/runsc"

[plugin.runtime.runtimes.runsc.options]

[plugin.runtime.runtimes.runc]
binary = "/usr/bin/runc"
EOF

mkdir -p \
  /var/lib/axnoded/root \
  /var/lib/axnoded/store \
  /var/lib/axnoded/rootfs \
  /var/lib/axnoded/filestore \
  /run/axnoded \
  /tmp/runsc

AXNODED_PID=""
rootfs_staging_dir=""
cleanup() {
  if [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    kill "${AXNODED_PID}" >/dev/null 2>&1 || true
    wait "${AXNODED_PID}" >/dev/null 2>&1 || true
  fi
  stop_node_runtime_volumed
  umount /opt/nginx-rootfs >/dev/null 2>&1 || true
  if [ -n "${rootfs_staging_dir}" ]; then
    umount "${rootfs_staging_dir}" >/dev/null 2>&1 || true
    rmdir "${rootfs_staging_dir}" >/dev/null 2>&1 || true
  fi
  rm -f "${VERIFY_NGINX_ROOTFS_IMAGE}"
}
trap cleanup EXIT

# The image-baked nginx fixture is a subdirectory of Docker's OverlayFS. Its
# host-side lower paths are not reachable from this mount namespace, so replaying
# that lower chain would be unsafe. Give the demo the same independent readonly
# backing used by the runtime truth-path verification.
rootfs_staging_dir="$(mktemp -d /tmp/axnoded-dashboard-rootfs-staging.XXXXXX)"
truncate -s 536870912 "${VERIFY_NGINX_ROOTFS_IMAGE}"
mkfs.ext4 -q -F "${VERIFY_NGINX_ROOTFS_IMAGE}"
mount -o loop "${VERIFY_NGINX_ROOTFS_IMAGE}" "${rootfs_staging_dir}"
cp -a /opt/nginx-rootfs/. "${rootfs_staging_dir}/"
umount "${rootfs_staging_dir}"
rmdir "${rootfs_staging_dir}"
rootfs_staging_dir=""
mount -o loop,ro "${VERIFY_NGINX_ROOTFS_IMAGE}" /opt/nginx-rootfs

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
