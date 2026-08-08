#!/usr/bin/env bash
set -euo pipefail

IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
VOLUMED_SOCKET="${VOLUMED_SOCKET:-/run/volumed/volumed.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
AXNODED_GRPC_ADDRESS="${AXNODED_GRPC_ADDRESS:-}"
AXNODED_HTTP_ADDRESS="${AXNODED_HTTP_ADDRESS:-0.0.0.0:23001}"
AXNODED_FILESTORE_DIR="${AXNODED_FILESTORE_DIR:-/var/lib/axnoded/filestore}"
AXNODED_FILESTORE_MODE="${AXNODED_FILESTORE_MODE:-loopback_dev}"
AXNODED_FILESTORE_LOOPBACK_IMAGE="${AXNODED_FILESTORE_LOOPBACK_IMAGE:-${AXNODED_FILESTORE_DIR}.xfs.img}"
# The all-in-one node is used by sequential smoke suites that intentionally
# retain completed run bundles for diagnostics. Keep the development loopback
# sparse, but size its logical capacity for multiple default 256 MiB writable
# layers so node-local admission remains meaningful across the full suite.
AXNODED_FILESTORE_LOOPBACK_SIZE_BYTES="${AXNODED_FILESTORE_LOOPBACK_SIZE_BYTES:-8589934592}"
AXNODED_FILESTORE_SYSTEM_RESERVE_BYTES="${AXNODED_FILESTORE_SYSTEM_RESERVE_BYTES:-67108864}"
AXNODED_WRITABLE_LAYER_DEFAULT_LIMIT_BYTES="${AXNODED_WRITABLE_LAYER_DEFAULT_LIMIT_BYTES:-268435456}"
OBJECT_STORE_ENABLED="${OBJECT_STORE_ENABLED:-false}"
OBJECT_STORE_SCHEME="${OBJECT_STORE_SCHEME:-https}"
OBJECT_STORE_ENDPOINT="${OBJECT_STORE_ENDPOINT:-}"
OBJECT_STORE_ACCESS_KEY="${OBJECT_STORE_ACCESS_KEY:-}"
OBJECT_STORE_SECRET_KEY="${OBJECT_STORE_SECRET_KEY:-}"
OBJECT_STORE_BUCKET="${OBJECT_STORE_BUCKET:-}"
OBJECT_STORE_REGION="${OBJECT_STORE_REGION:-}"
OBJECT_STORE_SKIP_VERIFY="${OBJECT_STORE_SKIP_VERIFY:-false}"
REGISTRY_PROXY_URL="${REGISTRY_PROXY_URL:-}"
REGISTRY_PROXY_HEALTH_URL="${REGISTRY_PROXY_HEALTH_URL:-}"
REGISTRY_BLOB_URL_SCHEME="${REGISTRY_BLOB_URL_SCHEME:-https}"
REGISTRY_PROXY_FALLBACK="${REGISTRY_PROXY_FALLBACK:-true}"
REGISTRY_PROXY_CA_CERT="${REGISTRY_PROXY_CA_CERT:-}"
REGISTRY_MIRROR_URL="${REGISTRY_MIRROR_URL:-}"
NYDUS_READAHEAD_WORKERS="${NYDUS_READAHEAD_WORKERS:-0}"
NYDUS_READAHEAD_WINDOW_BYTES="${NYDUS_READAHEAD_WINDOW_BYTES:-33554432}"
NYDUS_DECODED_CACHE_BYTES="${NYDUS_DECODED_CACHE_BYTES:-8388608}"
REGISTRY_AUTHS_SOURCE="${REGISTRY_AUTHS_SOURCE:-}"
IMAGEFSD_CHUNK_SERVER_SOCK="${IMAGEFSD_CHUNK_SERVER_SOCK:-}"
IMAGEFSD_CHUNK_SERVER_LISTEN_PORT="${IMAGEFSD_CHUNK_SERVER_LISTEN_PORT:-9876}"
NAT_BACKEND="${NAT_BACKEND:-iptables}"
AXNODED_IDLE_RUNTIME_RETENTION_TTL="${AXNODED_IDLE_RUNTIME_RETENTION_TTL:-5m}"
AXNODED_IDLE_RUNTIME_RETENTION_MAX="${AXNODED_IDLE_RUNTIME_RETENTION_MAX:-8}"
AXNODED_DNS_NAMESERVERS="${AXNODED_DNS_NAMESERVERS:-}"
AXNODED_DNS_SEARCH_DOMAINS="${AXNODED_DNS_SEARCH_DOMAINS:-}"
AXNODED_DNS_OPTIONS="${AXNODED_DNS_OPTIONS:-}"
AXNODED_RESOURCE_POOL_RECONCILE_INTERVAL="${AXNODED_RESOURCE_POOL_RECONCILE_INTERVAL:-1s}"
AXNODED_NETWORK_IP_RANGE="${AXNODED_NETWORK_IP_RANGE:-172.17.0.1/16}"
AXNODED_CGROUP_CACHE_SIZE="${AXNODED_CGROUP_CACHE_SIZE:-16}"
AXNODED_INTERFACE_CACHE_SIZE="${AXNODED_INTERFACE_CACHE_SIZE:-16}"
AXNODED_MAX_INSTANCE_NUM="${AXNODED_MAX_INSTANCE_NUM:-64}"
BPFNET_PIN_PATH="${BPFNET_PIN_PATH:-/sys/fs/bpf/axern/bpfnet}"
BPFNET_MAP_SIZE="${BPFNET_MAP_SIZE:-16384}"
BPFNET_SNAT_MAP_SIZE="${BPFNET_SNAT_MAP_SIZE:-262144}"
BPFNET_SNAT_GC_INTERVAL="${BPFNET_SNAT_GC_INTERVAL:-1s}"
BPFNET_SNAT_TCP_IDLE_TIMEOUT="${BPFNET_SNAT_TCP_IDLE_TIMEOUT:-5m}"
BPFNET_SNAT_TCP_CLOSING_TIMEOUT="${BPFNET_SNAT_TCP_CLOSING_TIMEOUT:-2s}"
BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT="${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT:-10s}"
BPFNET_UPLINK_DEVICES="${BPFNET_UPLINK_DEVICES:-}"
BPFNET_LOCAL_OUT_COMPAT="${BPFNET_LOCAL_OUT_COMPAT:-true}"
BPFNET_IPTABLES_FALLBACK="${BPFNET_IPTABLES_FALLBACK:-true}"
AXNODED_CONTROL_PLANE_TARGET="${AXNODED_CONTROL_PLANE_TARGET:-}"
AXNODED_CONTROL_PLANE_NODE_ID="${AXNODED_CONTROL_PLANE_NODE_ID:-}"
AXNODED_CONTROL_PLANE_NODE_TARGET="${AXNODED_CONTROL_PLANE_NODE_TARGET:-}"
AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN="${AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN:-}"
AXNODED_CONTROL_PLANE_HEARTBEAT_INTERVAL="${AXNODED_CONTROL_PLANE_HEARTBEAT_INTERVAL:-5s}"
AXNODED_CONTROL_PLANE_NODE_RESOURCE_SOURCE="${AXNODED_CONTROL_PLANE_NODE_RESOURCE_SOURCE:-host}"
AXNODED_CONTROL_PLANE_KUBERNETES_NODE_NAME="${AXNODED_CONTROL_PLANE_KUBERNETES_NODE_NAME:-}"
AXNODED_CONTROL_PLANE_TLS_CA_CERT="${AXNODED_CONTROL_PLANE_TLS_CA_CERT:-}"
AXNODED_CONTROL_PLANE_TLS_CERT="${AXNODED_CONTROL_PLANE_TLS_CERT:-}"
AXNODED_CONTROL_PLANE_TLS_KEY="${AXNODED_CONTROL_PLANE_TLS_KEY:-}"
NODE_TUNNELD_ENABLED="${NODE_TUNNELD_ENABLED:-true}"
NODE_TUNNELD_LOG="${NODE_TUNNELD_LOG:-/var/log/axnoded/node-tunneld.log}"

AXNODED_ROOT="/var/lib/axnoded"
IMAGEMGR_ROOT="/var/lib/imagemgr"
VOLUMED_ROOT="/var/lib/volumed"
VOLUMED_LOCAL_ROOT="${VOLUMED_LOCAL_ROOT:-${VOLUMED_ROOT}/local}"
AXNODED_CONFIG="/tmp/axnoded-node-config.toml"
OSS_TEMPLATE="/tmp/imagemgr-oss-template.json"
NYDUS_TEMPLATE="/tmp/imagemgr-nydus-template.json"
OSS_AUTHS="/tmp/imagemgr-oss-auths.json"
REGISTRY_AUTHS="/tmp/imagemgr-registry-auths.json"
AXNODED_LOG="/var/log/axnoded/axnoded.log"
IMAGEFSD_CHUNK_DB_DIR="${IMAGEMGR_ROOT}/chunk_db"
IMAGEFSD_LOG="${IMAGEMGR_ROOT}/logs/imagefsd.log"
if [ -z "${IMAGEFSD_CHUNK_SERVER_SOCK}" ]; then
  IMAGEFSD_CHUNK_SERVER_SOCK="${IMAGEFSD_CHUNK_DB_DIR}/chunkserver.sock"
fi

mkdir -p \
  "$(dirname "${IMAGEMGR_SOCKET}")" \
  "$(dirname "${VOLUMED_SOCKET}")" \
  "$(dirname "${AXNODED_SOCKET}")" \
  "$(dirname "${IMAGEFSD_CHUNK_SERVER_SOCK}")" \
  "${AXNODED_ROOT}/root" \
  "${AXNODED_ROOT}/store" \
  "${AXNODED_ROOT}/rootfs" \
  "${IMAGEMGR_ROOT}" \
  "${IMAGEMGR_ROOT}/logs" \
  "${VOLUMED_ROOT}" \
  "${VOLUMED_LOCAL_ROOT}" \
  "$(dirname "${AXNODED_LOG}")" \
  /etc/axnoded \
  /tmp/runsc

ensure_runtime_base_spec() {
  local runtime_bin="$1"
  local output_path="$2"

  if [ -f "${output_path}" ] || [ ! -x "${runtime_bin}" ]; then
    return 0
  fi

  local tmpdir
  tmpdir="$(mktemp -d)"
  (
    cd "${tmpdir}"
    "${runtime_bin}" spec >/dev/null 2>&1
    cp config.json "${output_path}"
  )
  rm -rf "${tmpdir}"
}

ensure_bpf_fs() {
  if [ "${NAT_BACKEND}" != "ebpf" ]; then
    return 0
  fi
  mkdir -p /sys/fs/bpf
  if ! mountpoint -q /sys/fs/bpf; then
    mount -t bpf bpffs /sys/fs/bpf
  fi
}

ensure_bpf_fs

ensure_loop_devices() {
  if command -v modprobe >/dev/null 2>&1; then
    modprobe loop >/dev/null 2>&1 || true
  fi
  if [ ! -e /dev/loop-control ]; then
    mknod /dev/loop-control c 10 237
    chmod 660 /dev/loop-control
  fi
  for minor in $(seq 0 7); do
    if [ ! -e "/dev/loop${minor}" ]; then
      mknod "/dev/loop${minor}" b 7 "${minor}"
      chmod 660 "/dev/loop${minor}"
    fi
  done
}

ensure_loop_devices
ensure_runtime_base_spec /usr/local/bin/runsc /etc/axnoded/runsc-config.json
ensure_runtime_base_spec /usr/bin/runc /etc/axnoded/runc-config.json

toml_array_from_csv() {
  local input="$1"
  local output="["
  local first=true
  local item
  IFS=',' read -r -a items <<<"${input}"
  for item in "${items[@]}"; do
    item="${item#"${item%%[![:space:]]*}"}"
    item="${item%"${item##*[![:space:]]}"}"
    if [ -z "${item}" ]; then
      continue
    fi
    item="${item//\\/\\\\}"
    item="${item//\"/\\\"}"
    if [ "${first}" = true ]; then
      first=false
    else
      output+=", "
    fi
    output+="\"${item}\""
  done
  output+="]"
  printf '%s\n' "${output}"
}

unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY
unset http_proxy https_proxy all_proxy no_proxy

cat > "${AXNODED_CONFIG}" <<EOF
rootDir = "${AXNODED_ROOT}/root"
storeDir = "${AXNODED_ROOT}/store"

[plugin]
control_plane_target = "${AXNODED_CONTROL_PLANE_TARGET}"
control_plane_node_id = "${AXNODED_CONTROL_PLANE_NODE_ID}"
control_plane_node_target = "${AXNODED_CONTROL_PLANE_NODE_TARGET}"
control_plane_node_auth_token = "${AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN}"
control_plane_heartbeat_interval = "${AXNODED_CONTROL_PLANE_HEARTBEAT_INTERVAL}"
control_plane_node_resource_source = "${AXNODED_CONTROL_PLANE_NODE_RESOURCE_SOURCE}"
control_plane_kubernetes_node_name = "${AXNODED_CONTROL_PLANE_KUBERNETES_NODE_NAME}"
control_plane_tls_ca_cert = "${AXNODED_CONTROL_PLANE_TLS_CA_CERT}"
control_plane_tls_cert = "${AXNODED_CONTROL_PLANE_TLS_CERT}"
control_plane_tls_key = "${AXNODED_CONTROL_PLANE_TLS_KEY}"

[plugin.network]
ip_range = "${AXNODED_NETWORK_IP_RANGE}"
nat_backend = "${NAT_BACKEND}"

[plugin.network.ebpf]
pin_path = "${BPFNET_PIN_PATH}"
map_size = ${BPFNET_MAP_SIZE}
snat_map_size = ${BPFNET_SNAT_MAP_SIZE}
snat_gc_interval = "${BPFNET_SNAT_GC_INTERVAL}"
snat_tcp_idle_timeout = "${BPFNET_SNAT_TCP_IDLE_TIMEOUT}"
snat_tcp_closing_timeout = "${BPFNET_SNAT_TCP_CLOSING_TIMEOUT}"
snat_datagram_idle_timeout = "${BPFNET_SNAT_DATAGRAM_IDLE_TIMEOUT}"
uplink_devices = $(toml_array_from_csv "${BPFNET_UPLINK_DEVICES}")
local_out_compat = ${BPFNET_LOCAL_OUT_COMPAT}
iptables_fallback = ${BPFNET_IPTABLES_FALLBACK}

[plugin.resource]
cgroup_cache_size = ${AXNODED_CGROUP_CACHE_SIZE}
interface_cache_size = ${AXNODED_INTERFACE_CACHE_SIZE}
cgroup_root_name = "/sandbox"
max_instance_num = ${AXNODED_MAX_INSTANCE_NUM}
resource_pool_reconcile_interval = "${AXNODED_RESOURCE_POOL_RECONCILE_INTERVAL}"
recycle_policy = "destroy"

[plugin.runtime]
image_lib_dir = "${AXNODED_ROOT}/rootfs"
image_manager_socket = "${IMAGEMGR_SOCKET}"
volume_manager_socket = "${VOLUMED_SOCKET}"
runtime_runner_binary = "/usr/local/libexec/axnoded/axnoded-runtime-runner"
cgroup_enforcement = "required"
filestore_dir = "${AXNODED_FILESTORE_DIR}"
filestore_mode = "${AXNODED_FILESTORE_MODE}"
filestore_loopback_image = "${AXNODED_FILESTORE_LOOPBACK_IMAGE}"
filestore_loopback_size_bytes = ${AXNODED_FILESTORE_LOOPBACK_SIZE_BYTES}
filestore_system_reserve_bytes = ${AXNODED_FILESTORE_SYSTEM_RESERVE_BYTES}
writable_layer_default_limit_bytes = ${AXNODED_WRITABLE_LAYER_DEFAULT_LIMIT_BYTES}
idle_runtime_retention_ttl = "${AXNODED_IDLE_RUNTIME_RETENTION_TTL}"
idle_runtime_retention_max = ${AXNODED_IDLE_RUNTIME_RETENTION_MAX}

[plugin.runtime.dns]
nameservers = $(toml_array_from_csv "${AXNODED_DNS_NAMESERVERS}")
search_domains = $(toml_array_from_csv "${AXNODED_DNS_SEARCH_DOMAINS}")
options = $(toml_array_from_csv "${AXNODED_DNS_OPTIONS}")

[plugin.runtime.runtimes.runsc]
binary = "/usr/local/bin/runsc"

[plugin.runtime.runtimes.runsc.options]
allow_suid = true

[plugin.runtime.runtimes.runc]
binary = "/usr/bin/runc"

[plugin.runtime.runtimes.runc.options]
EOF

jq -n \
  --arg scheme "${OBJECT_STORE_SCHEME}" \
  --arg endpoint "${OBJECT_STORE_ENDPOINT}" \
  --arg region "${OBJECT_STORE_REGION}" \
  --arg bucket "${OBJECT_STORE_BUCKET}" \
  --arg access_key "${OBJECT_STORE_ACCESS_KEY}" \
  --arg secret_key "${OBJECT_STORE_SECRET_KEY}" \
  --argjson skip_verify "${OBJECT_STORE_SKIP_VERIFY}" \
  '{
    type: "s3",
    s3: {
      scheme: $scheme,
      endpoint: $endpoint,
      region: $region,
      bucket_name: $bucket,
      object_prefix: "",
      access_key_id: $access_key,
      access_key_secret: $secret_key,
      skip_verify: $skip_verify,
      timeout: 30,
      connect_timeout: 5,
      retry_limit: 3
    }
  }' > "${OSS_TEMPLATE}"

case "${REGISTRY_PROXY_FALLBACK}" in
  true|false) ;;
  *)
    echo "REGISTRY_PROXY_FALLBACK must be true or false" >&2
    exit 1
    ;;
esac

if [ -n "${REGISTRY_PROXY_URL}" ]; then
  case "${REGISTRY_BLOB_URL_SCHEME}" in
    http|https) ;;
    *)
      echo "REGISTRY_BLOB_URL_SCHEME must be http or https when REGISTRY_PROXY_URL is set" >&2
      exit 1
      ;;
  esac
fi
if [ -n "${REGISTRY_PROXY_CA_CERT}" ] && [ ! -r "${REGISTRY_PROXY_CA_CERT}" ]; then
  echo "REGISTRY_PROXY_CA_CERT is not readable: ${REGISTRY_PROXY_CA_CERT}" >&2
  exit 1
fi

jq -n \
  --arg proxy_url "${REGISTRY_PROXY_URL}" \
  --arg proxy_health_url "${REGISTRY_PROXY_HEALTH_URL}" \
  --arg blob_url_scheme "${REGISTRY_BLOB_URL_SCHEME}" \
  --arg proxy_ca_cert "${REGISTRY_PROXY_CA_CERT}" \
  --argjson proxy_fallback "${REGISTRY_PROXY_FALLBACK}" \
  '{
    type: "registry",
    registry: {
      scheme: "https",
      host: "registry.invalid",
      repo: "placeholder/image",
      auth: "",
      skip_verify: false,
      timeout: 30,
      connect_timeout: 5,
      retry_limit: 3
    }
  }
  | if $proxy_url == "" then . else
      .registry.blob_url_scheme = $blob_url_scheme
      | if $proxy_ca_cert == "" then . else
          .registry.ca_cert_files = [$proxy_ca_cert]
        end
      | .registry.proxy = {
          url: $proxy_url,
          ping_url: $proxy_health_url,
          fallback: $proxy_fallback
        }
    end' > "${NYDUS_TEMPLATE}"

if [ "${OBJECT_STORE_ENABLED}" = "true" ]; then
  if [ -z "${OBJECT_STORE_ENDPOINT}" ] || [ -z "${OBJECT_STORE_BUCKET}" ]; then
    echo "OBJECT_STORE_ENDPOINT and OBJECT_STORE_BUCKET are required when OBJECT_STORE_ENABLED=true" >&2
    exit 1
  fi
  jq -n \
    --arg key "${OBJECT_STORE_ENDPOINT}/${OBJECT_STORE_BUCKET}" \
    --arg access_key "${OBJECT_STORE_ACCESS_KEY}" \
    --arg secret_key "${OBJECT_STORE_SECRET_KEY}" \
    '{($key): {access_key_id: $access_key, access_key_secret: $secret_key}}' > "${OSS_AUTHS}"
else
  echo '{}' > "${OSS_AUTHS}"
fi

if [ -n "${REGISTRY_AUTHS_SOURCE}" ] && [ -s "${REGISTRY_AUTHS_SOURCE}" ]; then
  cp "${REGISTRY_AUTHS_SOURCE}" "${REGISTRY_AUTHS}"
else
  cat > "${REGISTRY_AUTHS}" <<'EOF'
{}
EOF
fi
chmod 600 "${REGISTRY_AUTHS}"

/usr/local/bin/imagefsd --verbose serve-chunk \
  --chunk-db-dir "${IMAGEFSD_CHUNK_DB_DIR}" \
  --listen-port "${IMAGEFSD_CHUNK_SERVER_LISTEN_PORT}" \
  --chunk-server-sock "${IMAGEFSD_CHUNK_SERVER_SOCK}" \
  --log-file "${IMAGEFSD_LOG}" &
IMAGEFSD_PID=$!

/usr/local/bin/imagemgr \
  -root "${IMAGEMGR_ROOT}" \
  -node_id "${AXNODED_CONTROL_PLANE_NODE_ID}" \
  -imagefsd_bin /usr/local/bin/imagefsd \
  -oss_template "${OSS_TEMPLATE}" \
  -nydus_template "${NYDUS_TEMPLATE}" \
  -oss_auths_path "${OSS_AUTHS}" \
  -registry_auths_path "${REGISTRY_AUTHS}" \
  -registry_mirror_url "${REGISTRY_MIRROR_URL}" \
  -nydus_readahead_workers "${NYDUS_READAHEAD_WORKERS}" \
  -nydus_readahead_window_bytes "${NYDUS_READAHEAD_WINDOW_BYTES}" \
  -nydus_decoded_cache_bytes "${NYDUS_DECODED_CACHE_BYTES}" \
  -http_sock "${IMAGEMGR_SOCKET}" \
  -debug &
IMAGEMGR_PID=$!

/usr/local/bin/volumed \
  -root "${VOLUMED_ROOT}" \
  -socket "${VOLUMED_SOCKET}" \
  -local-root "${VOLUMED_LOCAL_ROOT}" &
VOLUMED_PID=$!

cleanup() {
  kill "${NODE_TUNNELD_SUPERVISOR_PID:-0}" >/dev/null 2>&1 || true
  kill "${AXNODED_PID:-0}" >/dev/null 2>&1 || true
  kill "${VOLUMED_PID:-0}" >/dev/null 2>&1 || true
  kill "${IMAGEMGR_PID:-0}" >/dev/null 2>&1 || true
  kill "${IMAGEFSD_PID:-0}" >/dev/null 2>&1 || true
  wait "${NODE_TUNNELD_SUPERVISOR_PID:-0}" >/dev/null 2>&1 || true
  wait "${AXNODED_PID:-0}" >/dev/null 2>&1 || true
  wait "${VOLUMED_PID:-0}" >/dev/null 2>&1 || true
  wait "${IMAGEMGR_PID:-0}" >/dev/null 2>&1 || true
  wait "${IMAGEFSD_PID:-0}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

run_node_tunneld_supervisor() {
  local child_pid=""
  trap 'kill "${child_pid:-0}" >/dev/null 2>&1 || true; wait "${child_pid:-0}" >/dev/null 2>&1 || true; exit 0' TERM INT
  while true; do
    /usr/local/bin/node-tunneld "$@" >>"${NODE_TUNNELD_LOG}" 2>&1 &
    child_pid=$!
    set +e
    wait "${child_pid}"
    rc=$?
    set -e
    child_pid=""
    echo "node-tunneld exited rc=${rc}; restarting in 2s" >>"${NODE_TUNNELD_LOG}"
    sleep 2
  done
}

for _ in $(seq 1 40); do
  if [ -S "${IMAGEMGR_SOCKET}" ]; then
    break
  fi
  sleep 1
done
if [ ! -S "${IMAGEMGR_SOCKET}" ]; then
  echo "imagemgr socket not ready: ${IMAGEMGR_SOCKET}" >&2
  exit 1
fi
for _ in $(seq 1 40); do
  if [ -S "${VOLUMED_SOCKET}" ]; then
    break
  fi
  sleep 1
done
if [ ! -S "${VOLUMED_SOCKET}" ]; then
  echo "volumed socket not ready: ${VOLUMED_SOCKET}" >&2
  exit 1
fi
for _ in $(seq 1 40); do
  if [ -S "${IMAGEFSD_CHUNK_SERVER_SOCK}" ]; then
    break
  fi
  sleep 1
done
if [ ! -S "${IMAGEFSD_CHUNK_SERVER_SOCK}" ]; then
  echo "imagefsd chunk server socket not ready: ${IMAGEFSD_CHUNK_SERVER_SOCK}" >&2
  exit 1
fi

axnoded_args=(
  -root "${AXNODED_ROOT}"
  -config "${AXNODED_CONFIG}"
  -socket "${AXNODED_SOCKET}"
  -http-address "${AXNODED_HTTP_ADDRESS}"
  -log-level debug
  -log-file "${AXNODED_LOG}"
)
if [ -n "${AXNODED_GRPC_ADDRESS}" ]; then
  axnoded_args+=(-grpc-address "${AXNODED_GRPC_ADDRESS}")
fi

/usr/local/bin/axnoded "${axnoded_args[@]}" &
AXNODED_PID=$!

for _ in $(seq 1 40); do
  if curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    if [ "${NODE_TUNNELD_ENABLED}" = "true" ] && [ -n "${AXNODED_CONTROL_PLANE_TARGET}" ] && [ -n "${AXNODED_CONTROL_PLANE_NODE_ID}" ]; then
      node_tunneld_args=(
        -node-id "${AXNODED_CONTROL_PLANE_NODE_ID}" \
        -node-auth-token "${AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN}" \
        -control-target "${AXNODED_CONTROL_PLANE_TARGET}" \
        -operator-socket "${AXNODED_SOCKET}" \
        -tls-ca-cert "${AXNODED_CONTROL_PLANE_TLS_CA_CERT}" \
        -tls-cert "${AXNODED_CONTROL_PLANE_TLS_CERT}" \
        -tls-key "${AXNODED_CONTROL_PLANE_TLS_KEY}" \
        -relay-tls-ca-cert "${AXNODED_CONTROL_PLANE_TLS_CA_CERT}"
      )
      : >"${NODE_TUNNELD_LOG}"
      run_node_tunneld_supervisor "${node_tunneld_args[@]}" &
      NODE_TUNNELD_SUPERVISOR_PID=$!
    else
      NODE_TUNNELD_SUPERVISOR_PID=""
    fi
    echo "node_all_in_one_ready=true"
    wait -n "${IMAGEFSD_PID}" "${IMAGEMGR_PID}" "${VOLUMED_PID}" "${AXNODED_PID}"
    exit $?
  fi
  sleep 1
done

echo "axnoded http readiness check failed" >&2
exit 1
