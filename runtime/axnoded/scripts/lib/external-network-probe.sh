#!/usr/bin/env bash

: "${EXTERNAL_NETWORK_PROBE_NETNS:=sbxext}"
: "${EXTERNAL_NETWORK_PROBE_HOST_DEV:=sbxext0}"
: "${EXTERNAL_NETWORK_PROBE_CLIENT_DEV:=sbxext1}"
: "${EXTERNAL_NETWORK_PROBE_HOST_ADDR:=198.19.0.1}"
: "${EXTERNAL_NETWORK_PROBE_CLIENT_ADDR:=198.19.0.2}"

ensure_bpf_fs() {
  local nat_backend="${1:-${NAT_BACKEND:-iptables}}"
  if [ "${nat_backend}" != "ebpf" ]; then
    return 0
  fi
  mkdir -p /sys/fs/bpf
  if ! mountpoint -q /sys/fs/bpf; then
    mount -t bpf bpffs /sys/fs/bpf
  fi
}

cleanup_external_probe() {
  ip netns del "${EXTERNAL_NETWORK_PROBE_NETNS}" >/dev/null 2>&1 || true
  ip link del "${EXTERNAL_NETWORK_PROBE_HOST_DEV}" >/dev/null 2>&1 || true
}

setup_external_probe() {
  cleanup_external_probe
  ip netns add "${EXTERNAL_NETWORK_PROBE_NETNS}"
  ip link add "${EXTERNAL_NETWORK_PROBE_HOST_DEV}" type veth peer name "${EXTERNAL_NETWORK_PROBE_CLIENT_DEV}"
  ip addr add "${EXTERNAL_NETWORK_PROBE_HOST_ADDR}/30" dev "${EXTERNAL_NETWORK_PROBE_HOST_DEV}"
  ip link set "${EXTERNAL_NETWORK_PROBE_HOST_DEV}" up
  ip link set "${EXTERNAL_NETWORK_PROBE_CLIENT_DEV}" netns "${EXTERNAL_NETWORK_PROBE_NETNS}"
  ip netns exec "${EXTERNAL_NETWORK_PROBE_NETNS}" ip link set lo up
  ip netns exec "${EXTERNAL_NETWORK_PROBE_NETNS}" ip addr add "${EXTERNAL_NETWORK_PROBE_CLIENT_ADDR}/30" dev "${EXTERNAL_NETWORK_PROBE_CLIENT_DEV}"
  ip netns exec "${EXTERNAL_NETWORK_PROBE_NETNS}" ip link set "${EXTERNAL_NETWORK_PROBE_CLIENT_DEV}" up
}

bpfnet_ebpf_uplinks_config() {
  local default_uplink="${1:?default uplink is required}"
  printf 'uplink_devices = ["%s", "%s"]' "${default_uplink}" "${EXTERNAL_NETWORK_PROBE_HOST_DEV}"
}
