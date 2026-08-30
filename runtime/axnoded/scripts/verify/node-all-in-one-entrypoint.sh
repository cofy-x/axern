#!/usr/bin/env bash
set -euo pipefail

export AXNODED_CONTROL_PLANE_NODE_ID="${AXNODED_CONTROL_PLANE_NODE_ID:-node-verify}"

VERIFY_ROOTFS_IMAGE="${VERIFY_ROOTFS_IMAGE:-/var/lib/axnoded/verify-rootfs.ext4}"
VERIFY_NGINX_ROOTFS_IMAGE="${VERIFY_NGINX_ROOTFS_IMAGE:-/var/lib/axnoded/verify-nginx-rootfs.ext4}"
child_pid=""
staging_dir=""

cleanup() {
  if [ -n "${child_pid}" ] && kill -0 "${child_pid}" >/dev/null 2>&1; then
    kill "${child_pid}" >/dev/null 2>&1 || true
    wait "${child_pid}" >/dev/null 2>&1 || true
  fi
  umount /opt/sample-rootfs >/dev/null 2>&1 || true
  umount /opt/nginx-rootfs >/dev/null 2>&1 || true
  if [ -n "${staging_dir}" ]; then
    umount "${staging_dir}" >/dev/null 2>&1 || true
    rmdir "${staging_dir}" >/dev/null 2>&1 || true
  fi
  rm -f "${VERIFY_ROOTFS_IMAGE}" "${VERIFY_NGINX_ROOTFS_IMAGE}"
}
trap cleanup EXIT
trap 'exit 143' TERM INT

# Kubernetes exposes only the loop device nodes that existed when a privileged
# container started. Prepare a bounded pool before creating either rootfs
# fixture; the production entrypoint runs later and cannot satisfy this
# prerequisite retroactively.
/usr/local/bin/axern-ensure-loop-devices 2

mount_readonly_fixture() {
  local source_dir="$1"
  local image="$2"
  local size_bytes="$3"

  staging_dir="$(mktemp -d /tmp/axnoded-rootfs-staging.XXXXXX)"
  truncate -s "${size_bytes}" "${image}"
  mkfs.ext4 -q -F "${image}"
  mount -o loop "${image}" "${staging_dir}"
  cp -a "${source_dir}/." "${staging_dir}/"
  umount "${staging_dir}"
  rmdir "${staging_dir}"
  staging_dir=""
  mount -o loop,ro "${image}" "${source_dir}"
}

mkdir -p /var/lib/axnoded
mount_readonly_fixture /opt/sample-rootfs "${VERIFY_ROOTFS_IMAGE}" 134217728
mount_readonly_fixture /opt/nginx-rootfs "${VERIFY_NGINX_ROOTFS_IMAGE}" 536870912

# Verification containers run the same fail-closed production entrypoint but
# use an explicit test-only reserve. The reserve must cover both the fixed
# 256 MiB runtime-conformance cgroup and the complete node-all-in-one daemon
# set; 512 MiB made capability publication depend on transient daemon memory.
# Production values come from a measured qualification receipt and must never
# inherit this harness value.
export AXNODED_MEMORY_SYSTEM_RESERVE_BYTES="${AXNODED_MEMORY_SYSTEM_RESERVE_BYTES:-805306368}"
/usr/local/bin/node-all-in-one-entrypoint "$@" &
child_pid=$!
wait "${child_pid}"
