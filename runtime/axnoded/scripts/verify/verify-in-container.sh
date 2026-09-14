#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
. "${ROOT_DIR}/scripts/lib/external-network-probe.sh"
. "${ROOT_DIR}/scripts/lib/node-runtime-services.sh"

RUNTIME_BINARY="${RUNTIME_BINARY:-/usr/local/bin/runsc}"
SOCKET_ADDRESS="${SOCKET_ADDRESS:-/run/axnoded/axnoded.sock}"
AXNODED_BIN="${AXNODED_BIN:-/usr/local/bin/axnoded}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
READY_TIMEOUT="${READY_TIMEOUT:-180}"
DEFAULT_UPLINK="${DEFAULT_UPLINK:-$(ip route show default | awk '/default/ {print $5; exit}')}"
AXNODED_IP_RANGE="${AXNODED_IP_RANGE:-172.31.0.1/16}"
VERIFY_ROOTFS_IMAGE="${VERIFY_ROOTFS_IMAGE:-/var/lib/axnoded/verify-rootfs.ext4}"
AXNODED_VERIFY_CGROUP_ENFORCEMENT="${AXNODED_VERIFY_CGROUP_ENFORCEMENT:-disabled_dev}"
case "${AXNODED_VERIFY_CGROUP_ENFORCEMENT}" in
  required) AXNODED_VERIFY_MEMORY_SYSTEM_RESERVE_BYTES="${AXNODED_VERIFY_MEMORY_SYSTEM_RESERVE_BYTES:-1073741824}" ;;
  disabled_dev) AXNODED_VERIFY_MEMORY_SYSTEM_RESERVE_BYTES=0 ;;
  *) echo "unsupported AXNODED_VERIFY_CGROUP_ENFORCEMENT=${AXNODED_VERIFY_CGROUP_ENFORCEMENT}" >&2; exit 1 ;;
esac
ensure_bpf_fs "${NAT_BACKEND}"

setup_external_probe

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
snat_map_size = 262144
${BPFNET_UPLINKS_CONFIG}
[plugin.resource]
cgroup_cache_size = 4
interface_cache_size = 4
cgroup_root_name = "sandbox"
max_instance_num = 8
memory_system_reserve_bytes = ${AXNODED_VERIFY_MEMORY_SYSTEM_RESERVE_BYTES}

[plugin.runtime]
image_lib_dir = "/var/lib/axnoded/rootfs"
image_manager_enabled = false
cgroup_enforcement = "${AXNODED_VERIFY_CGROUP_ENFORCEMENT}"
filestore_mode = "loopback_dev"
filestore_dir = "/var/lib/axnoded/filestore"
filestore_loopback_image = "/var/lib/axnoded/filestore.xfs"
filestore_loopback_size_bytes = 1073741824
filestore_system_reserve_bytes = 67108864
ephemeral_storage_default_limit_bytes = 268435456
EOF

cat >> /tmp/axnoded-config.toml <<EOF

[plugin.runtime.runsc]
binary = "${RUNTIME_BINARY}"
base_spec = "/etc/axnoded/runsc-config.json"
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
  umount /opt/sample-rootfs >/dev/null 2>&1 || true
  if [ -n "${rootfs_staging_dir}" ]; then
    umount "${rootfs_staging_dir}" >/dev/null 2>&1 || true
    rmdir "${rootfs_staging_dir}" >/dev/null 2>&1 || true
  fi
  rm -f "${VERIFY_ROOTFS_IMAGE}"
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
  echo "--- axnoded log tail ---" >&2
  tail -n 120 /tmp/axnoded.log >&2 || true
  exit 1
fi

if [ "${AXNODED_VERIFY_CGROUP_ENFORCEMENT}" = "required" ]; then
  axnoded_group="$(awk -F: '$1 == "0" { print $3 }' "/proc/${AXNODED_PID}/cgroup")"
  if [ "$(basename "${axnoded_group}")" != "internal" ]; then
    echo "axnoded cgroup ${axnoded_group} is not the reserved internal domain" >&2
    exit 1
  fi
  conformance_dir="/sys/fs/cgroup/$(dirname "${axnoded_group#/}")/conformance"
  [ "$(cat "${conformance_dir}/memory.max")" = "536870912" ]
  [ "$(cat "${conformance_dir}/memory.swap.max")" = "0" ]
  [ "$(cat "${conformance_dir}/memory.oom.group")" = "1" ]

  inventory=""
  contract_ready=false
  for _ in $(seq 1 160); do
    inventory="$(curl -fsS http://127.0.0.1:23001/inventoryz 2>/dev/null || true)"
    if jq -e --arg runtime "$(printf '%s' "runsc" | tr '[:lower:]' '[:upper:]')" '
      def available($name):
        [.node.capability_snapshot.observations[]?
          | select(.key.platform == $name and .state == "CAPABILITY_STATE_AVAILABLE")]
        | length == 1;
      .node.memory_budget.mode == "cgroup_v2" and
      .node.memory_budget.conformance_limit_bytes == 536870912 and
      .node.memory_budget.local_commitment_bytes == 0 and
      .node.memory_budget.conformance_commitment_bytes == 0 and
      .node.memory_budget.conformance_cleanup_debt_bytes == 0 and
      available("PLATFORM_CAPABILITY_" + $runtime + "_MEMORY_HARD_LIMIT") and
      available("PLATFORM_CAPABILITY_" + $runtime + "_EPHEMERAL_STORAGE_HARD_LIMIT")
    ' <<<"${inventory}" >/dev/null 2>&1; then
      contract_ready=true
      break
    fi
    sleep 1
  done
  if [ "${contract_ready}" != "true" ]; then
    echo "runtime conformance contract did not become observable" >&2
    jq '.node.memory_budget, .node.capability_snapshot.observations' <<<"${inventory}" >&2 || true
    tail -n 160 /tmp/axnoded.log >&2 || true
    exit 1
  fi
  echo "cgroup_conformance_contract_ok=true"
fi

assert_bpfnetctl_ready() {
  local phase="$1"
  local output
  local deadline
  output="$(mktemp)"
  deadline=$((SECONDS + READY_TIMEOUT))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if bpfnetctl check --json >"${output}" 2>&1 && jq -e '
      .ok == true and
      ([.checks[] | select(.name == "pinned_programs" and .ok == true)] | length == 1) and
      ([.checks[] | select(.name | startswith("program:"))] | length > 0) and
      ([.checks[] | select(.name == "pinned_programs" or (.name | startswith("program:"))) | select(.ok != true)] | length == 0)
    ' "${output}" >/dev/null 2>&1; then
      rm -f "${output}"
      echo "bpfnetctl_check_${phase}_ok=true"
      return 0
    fi
    sleep 1
  done
  echo "bpfnetctl did not become ready during ${phase}" >&2
  cat "${output}" >&2
  rm -f "${output}"
  return 1
}

if [ "${VERIFY_BPFNETCTL:-false}" = "true" ]; then
  assert_bpfnetctl_ready before_allocation
fi

ROOT_DIR="${ROOT_DIR}" SOCKET_ADDRESS="${SOCKET_ADDRESS}" \
  NAT_BACKEND="${NAT_BACKEND}" \
  bash "${ROOT_DIR}/scripts/verify/verify-generic-core.sh"
ROOT_DIR="${ROOT_DIR}" SOCKET_ADDRESS="${SOCKET_ADDRESS}" \
  NAT_BACKEND="${NAT_BACKEND}" \
  EXTERNAL_NETWORK_PROBE_NETNS="${EXTERNAL_NETWORK_PROBE_NETNS}" \
  EXTERNAL_NETWORK_PROBE_ADDR="${EXTERNAL_NETWORK_PROBE_HOST_ADDR}" \
  EXTERNAL_NETWORK_PROBE_CLIENT_ADDR="${EXTERNAL_NETWORK_PROBE_CLIENT_ADDR}" \
  bash "${ROOT_DIR}/scripts/verify/verify-runsc-profile.sh"

if [ "${VERIFY_BPFNETCTL:-false}" = "true" ]; then
  assert_bpfnetctl_ready after_allocation
fi

echo "verify_in_container_ok=true"
