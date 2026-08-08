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

/usr/local/bin/node-all-in-one-entrypoint "$@" &
child_pid=$!
wait "${child_pid}"
