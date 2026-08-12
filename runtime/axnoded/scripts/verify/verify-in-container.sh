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
VERIFY_ROOTFS_IMAGE="${VERIFY_ROOTFS_IMAGE:-/var/lib/axnoded/verify-rootfs.ext4}"
VERIFY_NGINX_ROOTFS_IMAGE="${VERIFY_NGINX_ROOTFS_IMAGE:-/var/lib/axnoded/verify-nginx-rootfs.ext4}"
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
cgroup_root_name = "sandbox"
max_instance_num = 8

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
ephemeral_storage_default_limit_bytes = 268435456
EOF

cat >> /tmp/axnoded-config.toml <<EOF

[plugin.runtime.runtimes.${RUNTIME_UNDER_TEST}]
binary = "${RUNTIME_BINARY}"
base_spec = "/etc/axnoded/${RUNTIME_UNDER_TEST}-config.json"
EOF

mkdir -p \
  /var/lib/axnoded/root \
  /var/lib/axnoded/store \
  /var/lib/axnoded/rootfs \
  /var/lib/axnoded/filestore \
  /run/axnoded \
  /tmp/runsc

# Axnoded loads every configured runtime before readiness. Materialize the
# explicit fail-closed base spec for both built-in handlers, not only the
# runtime selected by this verification profile.
ensure_node_runtime_base_spec "/usr/bin/runc" "/etc/axnoded/runc-config.json"
ensure_node_runtime_base_spec "/usr/local/bin/runsc" "/etc/axnoded/runsc-config.json"

AXNODED_PID=""
rootfs_staging_dir=""
cleanup() {
  if [ "${VERIFY_CAPTURE_CAPABILITY_SNAPSHOT:-false}" = "true" ] && \
     [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    curl -fsS http://127.0.0.1:23001/inventoryz > /tmp/axnoded-capability-inventory.json || true
  fi
  if [ -n "${AXNODED_PID}" ] && kill -0 "${AXNODED_PID}" >/dev/null 2>&1; then
    kill "${AXNODED_PID}" >/dev/null 2>&1 || true
    wait "${AXNODED_PID}" >/dev/null 2>&1 || true
  fi
  stop_node_runtime_volumed
  umount /opt/sample-rootfs >/dev/null 2>&1 || true
  umount /opt/nginx-rootfs >/dev/null 2>&1 || true
  if [ -n "${rootfs_staging_dir}" ]; then
    umount "${rootfs_staging_dir}" >/dev/null 2>&1 || true
    rmdir "${rootfs_staging_dir}" >/dev/null 2>&1 || true
  fi
  rm -f "${VERIFY_ROOTFS_IMAGE}"
  rm -f "${VERIFY_NGINX_ROOTFS_IMAGE}"
  if [ "${VERIFY_KEEP_EXTERNAL_PROBE:-false}" != "true" ]; then
    cleanup_external_probe
  fi
}
trap cleanup EXIT

# The image-baked fixture is a subdirectory of Docker's own OverlayFS. Its
# host-side lower paths are intentionally not reachable from this mount
# namespace, so it cannot be replayed safely as an OverlayFS lower chain.
# Materialize the fixture onto an independent, read-only ext4 mount instead.
rootfs_staging_dir="$(mktemp -d /tmp/axnoded-rootfs-staging.XXXXXX)"
truncate -s 134217728 "${VERIFY_ROOTFS_IMAGE}"
mkfs.ext4 -q -F "${VERIFY_ROOTFS_IMAGE}"
mount -o loop "${VERIFY_ROOTFS_IMAGE}" "${rootfs_staging_dir}"
cp -a /opt/sample-rootfs/. "${rootfs_staging_dir}/"
umount "${rootfs_staging_dir}"
rmdir "${rootfs_staging_dir}"
rootfs_staging_dir=""
mount -o loop,ro "${VERIFY_ROOTFS_IMAGE}" /opt/sample-rootfs

rootfs_staging_dir="$(mktemp -d /tmp/axnoded-nginx-rootfs-staging.XXXXXX)"
truncate -s 536870912 "${VERIFY_NGINX_ROOTFS_IMAGE}"
mkfs.ext4 -q -F "${VERIFY_NGINX_ROOTFS_IMAGE}"
mount -o loop "${VERIFY_NGINX_ROOTFS_IMAGE}" "${rootfs_staging_dir}"
cp -a /opt/nginx-rootfs/. "${rootfs_staging_dir}/"
umount "${rootfs_staging_dir}"
rmdir "${rootfs_staging_dir}"
rootfs_staging_dir=""
mount -o loop,ro "${VERIFY_NGINX_ROOTFS_IMAGE}" /opt/nginx-rootfs

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
