#!/usr/bin/env bash
set -euo pipefail

RUNTIME_UNDER_TEST="${RUNTIME_UNDER_TEST:-runsc}"
SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
VERIFY_NGINX_BIN="${VERIFY_NGINX_BIN:-/usr/local/bin/verify-nginx}"
VERIFY_EGRESS_BIN="${VERIFY_EGRESS_BIN:-/usr/local/bin/verify-egress}"
VERIFY_UDP_BIN="${VERIFY_UDP_BIN:-/usr/local/bin/verify-udp}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
BPFNET_STATE_DIR="${BPFNET_STATE_DIR:-/var/run/axern/bpfnet}"
EBPF_INGRESS_PROBE_NETNS="${EBPF_INGRESS_PROBE_NETNS:-}"
EBPF_INGRESS_PROBE_ADDR="${EBPF_INGRESS_PROBE_ADDR:-}"
EBPF_INGRESS_PROBE_CLIENT_ADDR="${EBPF_INGRESS_PROBE_CLIENT_ADDR:-}"
VERIFY_SKIP_LOCALHOST="${VERIFY_SKIP_LOCALHOST:-false}"
AXNODED_IP_RANGE="${AXNODED_IP_RANGE:-172.31.0.1/16}"
AXNODED_SNAT_CIDR="${AXNODED_SNAT_CIDR:-172.31.0.0/16}"

if [ "${RUNTIME_UNDER_TEST}" != "runsc" ]; then
  echo "runsc_profile_skipped=true runtime=${RUNTIME_UNDER_TEST}"
  exit 0
fi

verify_args=(
  -address "${SOCKET_ADDRESS}"
  -rootfs /opt/nginx-rootfs
  -runtime "${RUNTIME_UNDER_TEST}"
  -stdout /tmp/axnoded-nginx.stdout
  -stderr /tmp/axnoded-nginx.stderr
  -listen-port 18080
  -nat-backend "${NAT_BACKEND}"
)

if [ "${NAT_BACKEND}" = "ebpf" ] && [ -n "${EBPF_INGRESS_PROBE_NETNS}" ] && [ -n "${EBPF_INGRESS_PROBE_ADDR}" ]; then
  verify_args+=(
    -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
    -external-probe-address "${EBPF_INGRESS_PROBE_ADDR}"
  )
fi
if [ "${VERIFY_SKIP_LOCALHOST}" = "true" ]; then
  verify_args+=(-skip-localhost-check)
fi

"${VERIFY_NGINX_BIN}" "${verify_args[@]}"

udp_args=(
  -address "${SOCKET_ADDRESS}"
  -rootfs /opt/sample-rootfs
  -runtime "${RUNTIME_UNDER_TEST}"
  -stdout /tmp/axnoded-udp.stdout
  -stderr /tmp/axnoded-udp.stderr
  -listen-port 15353
  -target-port 1053
  -nat-backend "${NAT_BACKEND}"
  -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
  -external-probe-address "${EBPF_INGRESS_PROBE_ADDR}"
)
if [ "${NAT_BACKEND}" = "ebpf" ]; then
  udp_args+=(-bpfnet-pin-path /sys/fs/bpf/axern/bpfnet)
fi
"${VERIFY_UDP_BIN}" "${udp_args[@]}"

egress_args=(
  -address "${SOCKET_ADDRESS}"
  -rootfs /opt/sample-rootfs
  -runtime "${RUNTIME_UNDER_TEST}"
  -stdout /tmp/axnoded-egress.stdout
  -stderr /tmp/axnoded-egress.stderr
  -nat-backend "${NAT_BACKEND}"
  -external-probe-netns "${EBPF_INGRESS_PROBE_NETNS}"
  -external-probe-address "${EBPF_INGRESS_PROBE_CLIENT_ADDR}"
  -expected-source-ip "${EBPF_INGRESS_PROBE_ADDR}"
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
