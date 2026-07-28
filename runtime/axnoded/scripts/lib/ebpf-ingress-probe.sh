#!/usr/bin/env bash

: "${EBPF_INGRESS_PROBE_NETNS:=sbxext}"
: "${EBPF_INGRESS_PROBE_HOST_DEV:=sbxext0}"
: "${EBPF_INGRESS_PROBE_CLIENT_DEV:=sbxext1}"
: "${EBPF_INGRESS_PROBE_HOST_ADDR:=198.19.0.1}"
: "${EBPF_INGRESS_PROBE_CLIENT_ADDR:=198.19.0.2}"

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

cleanup_ebpf_ingress_probe() {
  ip netns del "${EBPF_INGRESS_PROBE_NETNS}" >/dev/null 2>&1 || true
  ip link del "${EBPF_INGRESS_PROBE_HOST_DEV}" >/dev/null 2>&1 || true
}

cleanup_external_probe() {
  cleanup_ebpf_ingress_probe
}

setup_ebpf_ingress_probe() {
  cleanup_ebpf_ingress_probe
  ip netns add "${EBPF_INGRESS_PROBE_NETNS}"
  ip link add "${EBPF_INGRESS_PROBE_HOST_DEV}" type veth peer name "${EBPF_INGRESS_PROBE_CLIENT_DEV}"
  ip addr add "${EBPF_INGRESS_PROBE_HOST_ADDR}/30" dev "${EBPF_INGRESS_PROBE_HOST_DEV}"
  ip link set "${EBPF_INGRESS_PROBE_HOST_DEV}" up
  ip link set "${EBPF_INGRESS_PROBE_CLIENT_DEV}" netns "${EBPF_INGRESS_PROBE_NETNS}"
  ip netns exec "${EBPF_INGRESS_PROBE_NETNS}" ip link set lo up
  ip netns exec "${EBPF_INGRESS_PROBE_NETNS}" ip addr add "${EBPF_INGRESS_PROBE_CLIENT_ADDR}/30" dev "${EBPF_INGRESS_PROBE_CLIENT_DEV}"
  ip netns exec "${EBPF_INGRESS_PROBE_NETNS}" ip link set "${EBPF_INGRESS_PROBE_CLIENT_DEV}" up
}

setup_external_probe() {
  setup_ebpf_ingress_probe
}

bpfnet_ebpf_uplinks_config() {
  local default_uplink="${1:?default uplink is required}"
  printf 'uplink_devices = ["%s", "%s"]' "${default_uplink}" "${EBPF_INGRESS_PROBE_HOST_DEV}"
}

ebpf_ingress_probe_command() {
  local container_name="${1:?container name is required}"
  local listen_port="${2:?listen port is required}"
  printf 'docker exec %s ip netns exec %s curl -fsS http://%s:%s/\n' \
    "${container_name}" "${EBPF_INGRESS_PROBE_NETNS}" "${EBPF_INGRESS_PROBE_HOST_ADDR}" "${listen_port}"
}
