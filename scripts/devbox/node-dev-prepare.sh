#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [ "$(uname -s)" != "Linux" ]; then
  echo "node-dev-prepare requires a Linux workspace. Run it inside 'make devbox-up'." >&2
  exit 1
fi

DEV_DIR="${ROOT_DIR}/.dev"
STACK_DIR="${DEV_DIR}/stack"
RUN_DIR="${DEV_DIR}/run"
BIN_DIR="${STACK_DIR}/bin"
AXNODED_DIR="${DEV_DIR}/axnoded"
IMAGEMGR_DIR="${DEV_DIR}/imagemgr"
VOLUMED_DIR="${DEV_DIR}/volumed"
IMAGEFSD_DIR="${DEV_DIR}/imagefsd"
CONTROL_PLANE_TARGET="${AXERN_DEV_CONTROL_PLANE_TARGET:-127.0.0.1:24000}"
CONTROL_PLANE_NODE_ID="${AXERN_DEV_CONTROL_PLANE_NODE_ID:-axern-dev-node}"
CONTROL_PLANE_NODE_TARGET="${AXERN_DEV_CONTROL_PLANE_NODE_TARGET:-127.0.0.1:23000}"
CONTROL_PLANE_NODE_AUTH_TOKEN="${AXERN_DEV_CONTROL_PLANE_NODE_AUTH_TOKEN:-axern-local-node-token}"
CONTROL_PLANE_TLS_CA_CERT="${AXERN_DEV_CONTROL_PLANE_TLS_CA_CERT:-${DEV_DIR}/certs/ca.crt}"
CONTROL_PLANE_TLS_CERT="${AXERN_DEV_CONTROL_PLANE_TLS_CERT:-${DEV_DIR}/certs/node.crt}"
CONTROL_PLANE_TLS_KEY="${AXERN_DEV_CONTROL_PLANE_TLS_KEY:-${DEV_DIR}/certs/node.key}"

if [ -e "${DEV_DIR}" ] && command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
  sudo chown -R "$(id -u):$(id -g)" "${DEV_DIR}" 2>/dev/null || true
fi

mkdir -p \
  "${RUN_DIR}" \
  "${BIN_DIR}" \
  "${DEV_DIR}/certs" \
  "${AXNODED_DIR}/root" \
  "${AXNODED_DIR}/store" \
  "${AXNODED_DIR}/rootfs" \
  "${AXNODED_DIR}/logs" \
  "${AXNODED_DIR}/filestore" \
  "${IMAGEMGR_DIR}" \
  "${VOLUMED_DIR}/local" \
  "${IMAGEFSD_DIR}/chunkdb"

bash "${ROOT_DIR}/scripts/dev-mtls-certs.sh" "${DEV_DIR}/certs" >/dev/null
go -C "${ROOT_DIR}/runtime/axnoded" build -o "${BIN_DIR}/axnoded-runtime-runner" ./cmd/axnoded-runtime-runner

cat > "${AXNODED_DIR}/config.toml" <<EOF
rootDir = "${AXNODED_DIR}/root"
storeDir = "${AXNODED_DIR}/store"

[plugin]
control_plane_target = "${CONTROL_PLANE_TARGET}"
control_plane_node_id = "${CONTROL_PLANE_NODE_ID}"
control_plane_node_target = "${CONTROL_PLANE_NODE_TARGET}"
control_plane_node_auth_token = "${CONTROL_PLANE_NODE_AUTH_TOKEN}"
control_plane_heartbeat_interval = "5s"
control_plane_tls_ca_cert = "${CONTROL_PLANE_TLS_CA_CERT}"
control_plane_tls_cert = "${CONTROL_PLANE_TLS_CERT}"
control_plane_tls_key = "${CONTROL_PLANE_TLS_KEY}"
control_plane_node_state = "ready"
control_plane_node_capabilities = [
  "feature:ports",
  "network:iptables",
]

[plugin.network]
ip_range = "172.17.0.1/16"
nat_backend = "iptables"

[plugin.resource]
cgroup_cache_size = 4
interface_cache_size = 4
cgroup_root_name = "/sandbox"
max_instance_num = 8
recycle_policy = "destroy"

[plugin.runtime]
image_lib_dir = "${AXNODED_DIR}/rootfs"
image_manager_socket = "${RUN_DIR}/imagemgr.sock"
volume_manager_socket = "${RUN_DIR}/volumed.sock"
runtime_runner_binary = "${BIN_DIR}/axnoded-runtime-runner"
cgroup_enforcement = "disabled_dev"
filestore_dir = "${AXNODED_DIR}/filestore"
filestore_mode = "loopback_dev"
filestore_loopback_image = "${AXNODED_DIR}/filestore.xfs.img"
filestore_loopback_size_bytes = 536870912
filestore_system_reserve_bytes = 67108864
ephemeral_storage_default_limit_bytes = 268435456

[plugin.runtime.runtimes.runsc]
binary = "/usr/local/bin/runsc"

[plugin.runtime.runtimes.runsc.options]
allow_suid = true

[plugin.runtime.runtimes.runc]
binary = "/usr/bin/runc"
EOF
