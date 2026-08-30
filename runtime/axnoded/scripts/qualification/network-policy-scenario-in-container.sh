#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Linux" ] || [ "$(id -u)" -ne 0 ]; then
  echo "network-policy scenario qualification requires a privileged Linux runner" >&2
  exit 1
fi

runtime_name=""
network_backend=""
ip_family=""
policy_mode=""
samples=""
concurrency=""
payload_bytes=""
sustained_seconds=""
rule_scale_counts=""
output=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --runtime) runtime_name="${2:?}"; shift 2 ;;
    --network-backend) network_backend="${2:?}"; shift 2 ;;
    --ip-family) ip_family="${2:?}"; shift 2 ;;
    --policy-mode) policy_mode="${2:?}"; shift 2 ;;
    --samples) samples="${2:?}"; shift 2 ;;
    --concurrency) concurrency="${2:?}"; shift 2 ;;
    --payload-bytes) payload_bytes="${2:?}"; shift 2 ;;
    --sustained-seconds) sustained_seconds="${2:?}"; shift 2 ;;
    --rule-scale-counts) rule_scale_counts="${2:?}"; shift 2 ;;
    --output) output="${2:?}"; shift 2 ;;
    *) echo "unknown qualification argument: $1" >&2; exit 1 ;;
  esac
done

for required in runtime_name network_backend ip_family policy_mode samples concurrency payload_bytes sustained_seconds rule_scale_counts output; do
  if [ -z "${!required}" ]; then
    echo "missing qualification argument: ${required}" >&2
    exit 1
  fi
done

case "${network_backend}" in
  bridge) nat_backend=iptables ;;
  ebpf) nat_backend=ebpf ;;
  *) echo "unsupported network backend: ${network_backend}" >&2; exit 1 ;;
esac

fixture_ns="axern-qual-fixture"
fixture_host_dev="axqhost0"
fixture_peer_dev="axqpeer0"
case "${ip_family}" in
  ipv4)
    fixture_host_ip="198.19.0.1"
    fixture_ip="198.19.0.2"
    node_range="172.31.0.1/16"
    ;;
  ipv6)
    fixture_host_ip="2001:db8:19::1"
    fixture_ip="2001:db8:19::2"
    node_range="fd31::1/64"
    ;;
  *) echo "unsupported IP family: ${ip_family}" >&2; exit 1 ;;
esac

node_pid=""
fixture_pid=""
cleanup() {
  if [ -n "${node_pid}" ] && kill -0 "${node_pid}" >/dev/null 2>&1; then
    kill "${node_pid}" >/dev/null 2>&1 || true
    wait "${node_pid}" >/dev/null 2>&1 || true
  fi
  if [ -n "${fixture_pid}" ] && kill -0 "${fixture_pid}" >/dev/null 2>&1; then
    kill "${fixture_pid}" >/dev/null 2>&1 || true
    wait "${fixture_pid}" >/dev/null 2>&1 || true
  fi
  ip netns del "${fixture_ns}" >/dev/null 2>&1 || true
  ip link del "${fixture_host_dev}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
mkdir -p /var/lib/axnoded /var/lib/egressd /var/lib/imagemgr /var/lib/volumed /run/axnoded /run/egressd
# This script only runs in a disposable qualification image. These exact
# directories contain state created by the preceding matrix cell in that image.
find /var/lib/axnoded /var/lib/egressd /var/lib/imagemgr /var/lib/volumed -mindepth 1 -delete
find /run/axnoded /run/egressd -mindepth 1 -delete

default_uplink="$(ip route show default | awk '/default/ {print $5; exit}')"
if [ -z "${default_uplink}" ]; then
  default_uplink="$(ip -6 route show default | awk '/default/ {print $5; exit}')"
fi
if [ -z "${default_uplink}" ]; then
  echo "qualification runner has no default uplink" >&2
  exit 1
fi

ip netns add "${fixture_ns}"
ip link add "${fixture_host_dev}" type veth peer name "${fixture_peer_dev}"
ip link set "${fixture_peer_dev}" netns "${fixture_ns}"
ip link set "${fixture_host_dev}" up
ip netns exec "${fixture_ns}" ip link set lo up
ip netns exec "${fixture_ns}" ip link set "${fixture_peer_dev}" up
if [ "${ip_family}" = ipv4 ]; then
  ip addr add "${fixture_host_ip}/30" dev "${fixture_host_dev}"
  ip netns exec "${fixture_ns}" ip addr add "${fixture_ip}/30" dev "${fixture_peer_dev}"
  ip netns exec "${fixture_ns}" ip route add default via "${fixture_host_ip}"
  sysctl -qw net.ipv4.ip_forward=1
else
  # These documentation-range addresses exist only for the disposable fixture.
  # Skip duplicate-address detection so the listener cannot race a tentative
  # address on freshly-created veth devices.
  ip -6 addr add "${fixture_host_ip}/64" dev "${fixture_host_dev}" nodad
  ip netns exec "${fixture_ns}" ip -6 addr add "${fixture_ip}/64" dev "${fixture_peer_dev}" nodad
  ip netns exec "${fixture_ns}" ip -6 route add default via "${fixture_host_ip}"
  sysctl -qw net.ipv6.conf.all.forwarding=1
fi

fixture_log="/tmp/network-policy-fixture-${ip_family}.log"
ip netns exec "${fixture_ns}" network-policy-fixture -listen-ip "${fixture_ip}" -answer-ip "${fixture_ip}" >"${fixture_log}" 2>&1 &
fixture_pid=$!
for _ in $(seq 1 80); do
  grep -q '^network_policy_fixture_ready=true$' "${fixture_log}" 2>/dev/null && break
  kill -0 "${fixture_pid}" >/dev/null 2>&1 || { cat "${fixture_log}" >&2; exit 1; }
  sleep 0.1
done
grep -q '^network_policy_fixture_ready=true$' "${fixture_log}" || { cat "${fixture_log}" >&2; exit 1; }

export NAT_BACKEND="${nat_backend}"
export AXNODED_NETWORK_IP_RANGE="${node_range}"
export AXNODED_DNS_NAMESERVERS="${fixture_ip}"
export BPFNET_UPLINK_DEVICES="${default_uplink},${fixture_host_dev}"
export NODE_TUNNELD_ENABLED=false
export AXNODED_CONTROL_PLANE_TARGET=""
# Keep the qualification sandbox independent from transient all-in-one daemon
# memory while reserving the fixed 256 MiB runtime-conformance domain.
export AXNODED_MEMORY_SYSTEM_RESERVE_BYTES="${AXNODED_MEMORY_SYSTEM_RESERVE_BYTES:-805306368}"

node_log="/tmp/network-policy-node-${runtime_name}-${network_backend}-${ip_family}-${policy_mode}.log"
/bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >"${node_log}" 2>&1 &
node_pid=$!
for _ in $(seq 1 180); do
  if [ -S /run/axnoded/axnoded.sock ] && curl -fsS http://127.0.0.1:23001/readyz >/dev/null 2>&1; then
    inventory="$(curl -fsS http://127.0.0.1:23001/inventoryz 2>/dev/null || true)"
    if jq -e '
      [.node.capability_snapshot.observations[]?
       | select((.key.platform == "PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT" or .key.platform == "PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT") and .state == "CAPABILITY_STATE_AVAILABLE")]
      | length == 2
    ' <<<"${inventory}" >/dev/null 2>&1; then
      break
    fi
  fi
  kill -0 "${node_pid}" >/dev/null 2>&1 || { tail -n 160 "${node_log}" >&2; exit 1; }
  sleep 1
done

if ! [ -S /run/axnoded/axnoded.sock ] || ! curl -fsS http://127.0.0.1:23001/readyz >/dev/null 2>&1; then
  tail -n 160 "${node_log}" >&2
  exit 1
fi

inventory="$(curl -fsS http://127.0.0.1:23001/inventoryz 2>/dev/null || true)"
if ! jq -e '
  [.node.capability_snapshot.observations[]?
   | select((.key.platform == "PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT" or .key.platform == "PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT") and .state == "CAPABILITY_STATE_AVAILABLE")]
  | length == 2
' <<<"${inventory}" >/dev/null 2>&1; then
  echo "network-policy enforcement capabilities did not become available" >&2
  tail -n 160 "${node_log}" >&2
  exit 1
fi

if ! verify-network-policy-qualification \
  --runtime "${runtime_name}" \
  --network-backend "${network_backend}" \
  --ip-family "${ip_family}" \
  --policy-mode "${policy_mode}" \
  --samples "${samples}" \
  --concurrency "${concurrency}" \
  --payload-bytes "${payload_bytes}" \
  --sustained-seconds "${sustained_seconds}" \
  --rule-scale-counts "${rule_scale_counts}" \
  --fixture-address "${fixture_ip}" \
  --dns-server "${fixture_ip}" \
  --output "${output}"; then
  echo "network-policy qualification scenario failed; dumping bounded dataplane diagnostics" >&2
  nft -a list table inet axern_egress 2>&1 | tail -n 240 >&2 || true
  ip -4 rule show 2>&1 | tail -n 80 >&2 || true
  ip -4 route show table 166 2>&1 | tail -n 80 >&2 || true
  ip -6 rule show 2>&1 | tail -n 80 >&2 || true
  ip -6 route show table 166 2>&1 | tail -n 80 >&2 || true
  tail -n 160 "${node_log}" >&2 || true
  exit 1
fi

# DeleteAllocation confirms that the runtime target has disappeared, while
# cgroup retirement is deliberately completed by the asynchronous resource
# GC. Do not stop axnoded or discard its durable ledger until that ownership
# has converged. Otherwise the next matrix cell can discover a kernel cgroup
# without the memory-capacity identity that made its commitment durable.
retirement_converged=false
inventory=""
for _ in $(seq 1 120); do
  inventory="$(curl -fsS http://127.0.0.1:23001/inventoryz 2>/dev/null || true)"
  if jq -e '
    .node.memory_budget.local_commitment_bytes == 0 and
    .node.memory_budget.cleanup_debt_bytes == 0 and
    .node.memory_budget.retiring_cgroup_count == 0
  ' <<<"${inventory}" >/dev/null 2>&1; then
    retirement_converged=true
    break
  fi
  kill -0 "${node_pid}" >/dev/null 2>&1 || { tail -n 160 "${node_log}" >&2; exit 1; }
  sleep 1
done
if [ "${retirement_converged}" != "true" ]; then
  echo "network-policy qualification resource retirement did not converge" >&2
  jq '.node.memory_budget' <<<"${inventory}" >&2 || true
  tail -n 160 "${node_log}" >&2 || true
  exit 1
fi

echo "network_policy_qualification_scenario_ok=${runtime_name}/${network_backend}/${ip_family}/${policy_mode}" >&2
