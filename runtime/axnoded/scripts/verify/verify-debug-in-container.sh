#!/usr/bin/env bash
set -euo pipefail

DEBUG_LOG_LINES="${DEBUG_LOG_LINES:-80}"
BPFNET_STATE_DIR="${BPFNET_STATE_DIR:-/var/run/axern/bpfnet}"
TARGET_IP="$(jq -r '.[0].targetIp // empty' "${BPFNET_STATE_DIR}/service_map.json" 2>/dev/null || true)"

set +e
VERIFY_CAPTURE_CAPABILITY_SNAPSHOT=true VERIFY_KEEP_EXTERNAL_PROBE=true bash /workspace/scripts/verify/verify-in-container.sh
code=$?
set -e

echo "verify_exit=${code}"
echo "--- nat postrouting ---"
iptables -t nat -S POSTROUTING || true
echo "--- nat prerouting ---"
iptables -t nat -S PREROUTING || true
echo "--- nat output ---"
iptables -t nat -S OUTPUT || true
echo "--- bpfnet dataplane state ---"
cat "${BPFNET_STATE_DIR}/dataplane_state.json" || true
echo "--- bpfnet service map ---"
cat "${BPFNET_STATE_DIR}/service_map.json" || true
echo "--- tc ingress filters ---"
tc -s filter show dev eth0 ingress || true
tc -s filter show dev sbxext0 ingress || true
echo "--- tc egress filters ---"
tc -s filter show dev eth0 egress || true
tc -s filter show dev sbxext0 egress || true
echo "--- eth0 ---"
ip addr show dev eth0 || true
echo "--- sbxext0 ---"
ip addr show dev sbxext0 || true
echo "--- route get 198.19.0.1 ---"
ip route get 198.19.0.1 || true
echo "--- route get 198.19.0.2 ---"
ip route get 198.19.0.2 || true
echo "--- sandbox0 ---"
ip link show sandbox0 || true
ip addr show sandbox0 || true
echo "--- bridge links ---"
bridge link || true
echo "--- sandbox bridge ports ---"
ip link show master sandbox0 || true
if [ -n "${TARGET_IP}" ]; then
  echo "--- route get ${TARGET_IP} ---"
  ip route get "${TARGET_IP}" || true
fi
echo "--- netns dir ---"
ls -la /var/run/netns || true
echo "--- stdout ---"
cat /tmp/axnoded-verify.stdout || true
echo "--- stderr ---"
cat /tmp/axnoded-verify.stderr || true
echo "--- axnoded capability snapshot ---"
jq '.node.capability_snapshot // empty' /tmp/axnoded-capability-inventory.json 2>/dev/null || true
echo "--- nginx stdout ---"
cat /tmp/axnoded-nginx.stdout || true
echo "--- nginx stderr ---"
cat /tmp/axnoded-nginx.stderr || true
echo "--- volumed log tail ---"
tail -n "${DEBUG_LOG_LINES}" /tmp/volumed.log || true
echo "--- axnoded log tail ---"
tail -n "${DEBUG_LOG_LINES}" /tmp/axnoded.log || true

exit "${code}"
