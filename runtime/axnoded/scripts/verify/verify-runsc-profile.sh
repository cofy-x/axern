#!/usr/bin/env bash
set -euo pipefail

SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
VERIFY_EGRESS_BIN="${VERIFY_EGRESS_BIN:-/usr/local/bin/verify-egress}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
BPFNET_STATE_DIR="${BPFNET_STATE_DIR:-/var/run/axern/bpfnet}"
EXTERNAL_NETWORK_PROBE_NETNS="${EXTERNAL_NETWORK_PROBE_NETNS:-}"
EXTERNAL_NETWORK_PROBE_ADDR="${EXTERNAL_NETWORK_PROBE_ADDR:-}"
EXTERNAL_NETWORK_PROBE_CLIENT_ADDR="${EXTERNAL_NETWORK_PROBE_CLIENT_ADDR:-}"
AXNODED_IP_RANGE="${AXNODED_IP_RANGE:-172.31.0.1/16}"
AXNODED_SNAT_CIDR="${AXNODED_SNAT_CIDR:-172.31.0.0/16}"

egress_args=(
  -address "${SOCKET_ADDRESS}"
  -rootfs /opt/sample-rootfs
  -stdout /tmp/axnoded-egress.stdout
  -stderr /tmp/axnoded-egress.stderr
  -nat-backend "${NAT_BACKEND}"
  -external-probe-netns "${EXTERNAL_NETWORK_PROBE_NETNS}"
  -external-probe-address "${EXTERNAL_NETWORK_PROBE_CLIENT_ADDR}"
  -expected-source-ip "${EXTERNAL_NETWORK_PROBE_ADDR}"
)
if [ "${NAT_BACKEND}" = "ebpf" ]; then
  egress_args+=(-bpfnet-pin-path /sys/fs/bpf/axern/bpfnet)
fi
"${VERIFY_EGRESS_BIN}" "${egress_args[@]}"

case "${NAT_BACKEND}" in
  iptables)
    iptables -t nat -S POSTROUTING | grep -- "-s ${AXNODED_SNAT_CIDR} -j MASQUERADE"
    ;;
  ebpf)
    test -f "${BPFNET_STATE_DIR}/dataplane_state.json"
    ;;
  *)
    echo "unsupported NAT_BACKEND=${NAT_BACKEND}" >&2
    exit 1
    ;;
esac
ip link show sandbox0 >/dev/null
