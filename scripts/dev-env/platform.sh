#!/usr/bin/env bash

# shellcheck source=../proxy-env.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/proxy-env.sh"

configure_kind_proxy() {
  local container_http_proxy container_https_proxy
  container_http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
  container_https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
  if [ -z "${container_http_proxy}" ] && [ -z "${container_https_proxy}" ]; then
    return 0
  fi
  export HTTP_PROXY="${container_http_proxy}"
  export HTTPS_PROXY="${container_https_proxy}"
  export http_proxy="${container_http_proxy}"
  export https_proxy="${container_https_proxy}"
  echo "kind_proxy_configured=true"
}

configure_compose_no_proxy() {
  local host_no_proxy
  host_no_proxy="$(append_no_proxy_entries "${NO_PROXY:-${no_proxy:-}}" "$(local_no_proxy_entries compose)")"
  export NO_PROXY="${host_no_proxy}"
  export no_proxy="${host_no_proxy}"
}

local_no_proxy_entries() {
  local mode="${1:-generic}"
  local common="localhost,127.0.0.1,::1,host.docker.internal,${LOCAL_REGISTRY_NAME},${LOCAL_REGISTRY_HOST},${LOCAL_REGISTRY_CLUSTER_HOST},.svc,.svc.cluster.local,.cluster.local,10.96.0.0/12,10.244.0.0/16,172.16.0.0/12,192.168.0.0/16"
  case "${mode}" in
    k8s)
      printf '%s\n' "${common},controld,controld.${K8S_NAMESPACE},controld.${K8S_NAMESPACE}.svc,controld.${K8S_NAMESPACE}.svc.cluster.local,storaged,storaged.${K8S_NAMESPACE},storaged.${K8S_NAMESPACE}.svc,storaged.${K8S_NAMESPACE}.svc.cluster.local,gatewayd,gatewayd.${K8S_NAMESPACE},gatewayd.${K8S_NAMESPACE}.svc,gatewayd.${K8S_NAMESPACE}.svc.cluster.local,tunneld,tunneld.${K8S_NAMESPACE},tunneld.${K8S_NAMESPACE}.svc,tunneld.${K8S_NAMESPACE}.svc.cluster.local,node-all-in-one,postgres,minio,otel-collector,otel-collector.${K8S_NAMESPACE},otel-collector.${K8S_NAMESPACE}.svc,otel-collector.${K8S_NAMESPACE}.svc.cluster.local,otel-lgtm"
      ;;
    compose)
      printf '%s\n' "${common},controld,storaged,gatewayd,tunneld,node,postgres,minio,otel-collector,otel-lgtm"
      ;;
    *)
      printf '%s\n' "${common}"
      ;;
  esac
}

k8s_proxy_env_args() {
  local http_proxy https_proxy no_proxy registry_proxy_url
  http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
  https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
  no_proxy="$(append_no_proxy_entries "${NO_PROXY:-${no_proxy:-}}" "$(local_no_proxy_entries k8s)")"
  registry_proxy_url="${https_proxy:-${http_proxy}}"
  printf -- "--from-literal=HTTP_PROXY=%s\n" "${http_proxy}"
  printf -- "--from-literal=HTTPS_PROXY=%s\n" "${https_proxy}"
  printf -- "--from-literal=NO_PROXY=%s\n" "${no_proxy}"
  printf -- "--from-literal=http_proxy=%s\n" "${http_proxy}"
  printf -- "--from-literal=https_proxy=%s\n" "${https_proxy}"
  printf -- "--from-literal=no_proxy=%s\n" "${no_proxy}"
  printf -- "--from-literal=REGISTRY_PROXY_URL=%s\n" "${registry_proxy_url}"
  printf -- "--from-literal=REGISTRY_NO_PROXY=%s\n" "${no_proxy}"
  printf -- "--from-literal=CONTROLD_INSECURE_REGISTRIES=%s\n" "${LOCAL_REGISTRY_HOST},${LOCAL_REGISTRY_CLUSTER_HOST}"
  printf -- "--from-literal=IMAGEMGR_INSECURE_REGISTRIES=%s\n" "${LOCAL_REGISTRY_HOST},${LOCAL_REGISTRY_CLUSTER_HOST}"
}

write_compose_env() {
  local secrets_master_key
  secrets_master_key="$(cat "$(secrets_master_key_file compose)")"
  local container_http_proxy container_https_proxy container_no_proxy registry_proxy_url
  local axnoded_dns_nameservers axnoded_node_id
  local tunneld_runtime_uid tunneld_runtime_gid
  container_http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
  container_https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
  container_no_proxy="$(append_no_proxy_entries "${NO_PROXY:-${no_proxy:-}}" "$(local_no_proxy_entries compose)")"
  registry_proxy_url="${container_https_proxy:-${container_http_proxy}}"
  axnoded_dns_nameservers="${AXNODED_DNS_NAMESERVERS:-}"
  axnoded_node_id="${AXNODED_CONTROL_PLANE_NODE_ID:-node-compose-local}"
  tunneld_runtime_uid="$(id -u)"
  tunneld_runtime_gid="$(id -g)"
  local otel_enabled otel_endpoint otel_resource_attrs
  otel_enabled="false"
  otel_endpoint=""
  otel_resource_attrs="deployment.environment=compose"
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    otel_enabled="true"
    otel_endpoint="http://otel-collector:4317"
  fi
  cat > "$(compose_env_file)" <<EOF
AXERN_ROOT=${AXERN_ROOT}
COMPOSE_STATE_DIR=${COMPOSE_STATE_DIR}
CONTROLD_IMAGE=${CONTROLD_IMAGE}
TUNNELD_IMAGE=${TUNNELD_IMAGE}
TUNNELD_RUNTIME_UID=${tunneld_runtime_uid}
TUNNELD_RUNTIME_GID=${tunneld_runtime_gid}
GATEWAYD_IMAGE=${GATEWAYD_IMAGE}
NODE_ALL_IN_ONE_IMAGE=${NODE_ALL_IN_ONE_IMAGE}
PYTHON311_RUNTIME_IMAGE=${PYTHON311_RUNTIME_IMAGE}
SERVER_BASE_RUNTIME_IMAGE=${SERVER_BASE_RUNTIME_IMAGE}
CODING_BASE_RUNTIME_IMAGE=${CODING_BASE_RUNTIME_IMAGE}
DESKTOP_BASE_RUNTIME_IMAGE=${DESKTOP_BASE_RUNTIME_IMAGE}
CLAUDE_CODE_BUNDLE_IMAGE=${CLAUDE_CODE_BUNDLE_IMAGE}
CODEX_BUNDLE_IMAGE=${CODEX_BUNDLE_IMAGE}
OTEL_COLLECTOR_IMAGE=${OTEL_COLLECTOR_IMAGE}
OTEL_LGTM_IMAGE=${OTEL_LGTM_IMAGE}
AXERN_SECRETS_MASTER_KEY=${secrets_master_key}
AXNODED_CONTROL_PLANE_NODE_ID=${axnoded_node_id}
CONTAINER_HTTP_PROXY=${container_http_proxy}
CONTAINER_HTTPS_PROXY=${container_https_proxy}
CONTAINER_NO_PROXY=${container_no_proxy}
REGISTRY_PROXY_URL=${registry_proxy_url}
CONTROLD_INSECURE_REGISTRIES=${LOCAL_REGISTRY_HOST},${LOCAL_REGISTRY_CLUSTER_HOST}
IMAGEMGR_INSECURE_REGISTRIES=${LOCAL_REGISTRY_HOST},${LOCAL_REGISTRY_CLUSTER_HOST}
AXNODED_DNS_NAMESERVERS=${axnoded_dns_nameservers}
CONTROLD_HTTP_PORT=${COMPOSE_CONTROLD_HTTP_PORT}
GATEWAY_CONTROL_PORT=${COMPOSE_GATEWAY_CONTROL_PORT}
GATEWAY_HTTP_PORT=${COMPOSE_GATEWAY_HTTP_PORT}
GATEWAY_SSH_PORT=${COMPOSE_GATEWAY_SSH_PORT}
POSTGRES_PORT=${COMPOSE_POSTGRES_PORT}
MINIO_API_PORT=${COMPOSE_MINIO_API_PORT}
MINIO_CONSOLE_PORT=${COMPOSE_MINIO_CONSOLE_PORT}
OTEL_ENABLED=${otel_enabled}
OTEL_EXPORTER_OTLP_ENDPOINT=${otel_endpoint}
OTEL_RESOURCE_ATTRIBUTES=${otel_resource_attrs}
OTEL_GRPC_PORT=${COMPOSE_OTEL_GRPC_PORT}
OTEL_HTTP_PORT=${COMPOSE_OTEL_HTTP_PORT}
LGTM_UI_PORT=${COMPOSE_LGTM_UI_PORT}
EOF
}

configure_compose_dns_verification() {
  local configured normalized
  configured="${AXERN_VERIFY_DNS_NAMESERVERS:-}"
  if [ -z "${configured//[[:space:]]/}" ]; then
    export AXNODED_DNS_NAMESERVERS=""
    echo "compose_dns_source=node_effective"
    return 0
  fi
  normalized="$(python3 - "${configured}" <<'PY'
import ipaddress
import sys

seen = set()
normalized = []
for item in sys.argv[1].split(","):
    value = item.strip()
    try:
        address = ipaddress.ip_address(value)
    except ValueError:
        raise SystemExit("AXERN_VERIFY_DNS_NAMESERVERS contains an invalid IP address")
    if address.is_loopback or address.is_unspecified:
        raise SystemExit("AXERN_VERIFY_DNS_NAMESERVERS contains a loopback or unspecified address")
    canonical = str(address)
    if canonical not in seen:
        seen.add(canonical)
        normalized.append(canonical)
if not normalized:
    raise SystemExit("AXERN_VERIFY_DNS_NAMESERVERS contains no usable resolver")
print(",".join(normalized))
PY
)" || return $?
  export AXNODED_DNS_NAMESERVERS="${normalized}"
  echo "compose_dns_source=verification_override"
}

ensure_local_images() {
  require_cmd docker
  case "${AXERN_IMAGE_MODE:-source}" in
    source) ;;
    release)
      source "${AXERN_ROOT}/scripts/release/images.sh"
      axern_export_release_images
      axern_release_images | xargs -n 1 -P 4 sh -c '
        echo "Pulling $1"
        docker pull "$1" >/dev/null
      ' sh
      echo "release_images_ready=true"
      return 0
      ;;
    *)
      echo "AXERN_IMAGE_MODE must be source or release" >&2
      return 2
      ;;
  esac
  if [ "${AXERN_SKIP_LOCAL_IMAGES_BUILD:-0}" = "1" ] || [ "${AXERN_SKIP_LOCAL_IMAGES_BUILD:-0}" = "true" ]; then
    echo "local_images_build_skipped=true"
    return 0
  fi
  bash "${AXERN_ROOT}/scripts/dev-env/build-images.sh"
}

ensure_host_image() {
  local image_ref="$1"
  if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
    docker pull "${image_ref}" >/dev/null
  fi
}

wait_for_node_summary() {
  local nodesz_url="$1"
  local node_id="$2"
  local timeout="${3:-120}"
  wait_for_node_summaries "${nodesz_url}" "${timeout}" "${node_id}"
}

wait_for_node_summaries() {
  local nodesz_url="$1"
  local timeout="$2"
  shift 2
  local node_ids=("$@")
  local started_at=${SECONDS}
  local next_report=$((SECONDS + 10))
  local deadline=$((SECONDS + timeout))
  while true; do
    local body
    body="$(curl --noproxy 'localhost,127.0.0.1,::1' --connect-timeout 2 --max-time 5 -fsS "${nodesz_url}" 2>/dev/null || true)"
    if printf '%s' "${body}" | python3 -c '
import json
import sys

expected = set(sys.argv[1:])
if not expected:
    raise SystemExit(1)

try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)

ready = set()
for node in payload.get("nodes", []):
    node_id = node.get("node_id")
    if node_id not in expected:
        continue
    axnoded = ((node.get("summary") or {}).get("components") or {}).get("axnoded") or {}
    runtime_slots = (((node.get("summary") or {}).get("pools") or {}).get("runtime_slots"))
    if (
        node.get("fresh")
        and node.get("summary_fresh")
        and axnoded.get("state") == 1
        and axnoded.get("ready")
        and runtime_slots is not None
    ):
        ready.add(node_id)

raise SystemExit(0 if ready == expected else 1)
' "${node_ids[@]}"; then
      return 0
    fi
    if [ "${SECONDS}" -ge "${deadline}" ]; then
      echo "wait_ready_timeout=node_summary elapsed_seconds=$((SECONDS - started_at)) timeout_seconds=${timeout} expected_nodes=${#node_ids[@]}" >&2
      return 1
    fi
    if [ "${SECONDS}" -ge "${next_report}" ]; then
      echo "wait_ready_pending=node_summary elapsed_seconds=$((SECONDS - started_at)) timeout_seconds=${timeout} expected_nodes=${#node_ids[@]}"
      next_report=$((SECONDS + 10))
    fi
    sleep 1
  done
}

compose_project_container_ids() {
  docker ps -a --filter "label=com.docker.compose.project=${COMPOSE_PROJECT_NAME}" -q 2>/dev/null || true
}

wait_for_compose_project_settle() {
  local timeout="${1:-60}"
  local deadline=$((SECONDS + timeout))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if [ -z "$(compose_project_container_ids)" ]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

compose_project_up() {
  local compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
  local compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}")
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    compose_args+=(--profile otel)
  fi
  local infra_services=(postgres minio)
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    infra_services+=(otel-collector otel-lgtm)
  fi
  docker compose "${compose_args[@]}" stop gatewayd node tunneld controld-retention controld storaged >/dev/null 2>&1 || true
  docker compose "${compose_args[@]}" up -d --remove-orphans "${infra_services[@]}"
  docker compose "${compose_args[@]}" rm -sf controld-migrate controld-access-bootstrap >/dev/null 2>&1 || true
  docker compose "${compose_args[@]}" up --force-recreate --exit-code-from controld-access-bootstrap controld-access-bootstrap
  docker compose "${compose_args[@]}" up -d --force-recreate --no-deps --remove-orphans storaged controld controld-retention tunneld node gatewayd
}

compose_project_reset_state() {
  local compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
  local compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}")
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    compose_args+=(--profile otel)
  fi
  docker compose "${compose_args[@]}" stop \
    gatewayd node tunneld controld-retention controld storaged controld-access-bootstrap controld-migrate postgres minio >/dev/null 2>&1 || true
  docker compose "${compose_args[@]}" rm -sf controld-access-bootstrap controld-migrate postgres minio >/dev/null 2>&1 || true
  rm -rf "${COMPOSE_STATE_DIR}/postgres" "${COMPOSE_STATE_DIR}/minio" "${COMPOSE_STATE_DIR}/run"
  ensure_state_dirs
}

compose_project_down() {
  local compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
  if [ ! -f "$(compose_env_file)" ]; then
    return 0
  fi
  local compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}")
  compose_args+=(--profile otel)
  local down_args=(down --remove-orphans)
  if [ "${1:-}" = "--purge" ]; then
    down_args+=("--volumes")
  fi
  docker compose "${compose_args[@]}" "${down_args[@]}"
  wait_for_compose_project_settle 90 || {
    echo "compose containers did not finish removing in time" >&2
    return 1
  }
}

detect_local_cluster_loader() {
  local context
  context="$(KUBECONFIG="$(with_kubeconfig)" kubectl config current-context 2>/dev/null || true)"
  if [[ "${context}" == kind-* ]]; then
    printf 'kind\n'
    return 0
  fi
  if [[ "${context}" == k3d-* ]]; then
    printf 'k3d\n'
    return 0
  fi
  if [[ "${context}" == minikube ]]; then
    printf 'minikube\n'
    return 0
  fi
  local nodes
  nodes="$(KUBECONFIG="$(with_kubeconfig)" kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)"
  if printf '%s\n' "${nodes}" | grep -q '^kind-'; then
    printf 'kind\n'
    return 0
  fi
  if printf '%s\n' "${nodes}" | grep -q '^k3d-'; then
    printf 'k3d\n'
    return 0
  fi
  if KUBECONFIG="$(with_kubeconfig)" kubectl get nodes -o jsonpath='{.items[0].metadata.labels.minikube\.k8s\.io/name}' 2>/dev/null | grep -q .; then
    printf 'minikube\n'
    return 0
  fi
  printf 'none\n'
}

load_image_to_cluster() {
  local image_ref="$1"
  case "$(detect_local_cluster_loader)" in
    kind)
      require_cmd kind
      kind load docker-image "${image_ref}" --name "${K8S_CLUSTER_NAME}"
      ;;
    k3d)
      require_cmd k3d
      KUBECONFIG="$(with_kubeconfig)" k3d image import "${image_ref}" -c "$(KUBECONFIG="$(with_kubeconfig)" kubectl config current-context | sed 's/^k3d-//')"
      ;;
    minikube)
      require_cmd minikube
      minikube image load "${image_ref}"
      ;;
    none)
      echo "warning: could not detect a supported local cluster loader; assuming the cluster can already resolve local images ${image_ref}" >&2
      ;;
  esac
}

ensure_k8s_images_loaded() {
  load_image_to_cluster "${CONTROLD_IMAGE}"
  load_image_to_cluster "${TUNNELD_IMAGE}"
  load_image_to_cluster "${GATEWAYD_IMAGE}"
  load_image_to_cluster "${NODE_ALL_IN_ONE_IMAGE}"
  load_image_to_cluster "${PYTHON311_RUNTIME_IMAGE}"
  load_image_to_cluster "${SERVER_BASE_RUNTIME_IMAGE}"
  load_image_to_cluster "${CODING_BASE_RUNTIME_IMAGE}"
  load_image_to_cluster "${DESKTOP_BASE_RUNTIME_IMAGE}"
  load_image_to_cluster "${CLAUDE_CODE_BUNDLE_IMAGE}"
  load_image_to_cluster "${CODEX_BUNDLE_IMAGE}"
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    ensure_host_image "${OTEL_COLLECTOR_IMAGE}"
    ensure_host_image "${OTEL_LGTM_IMAGE}"
    load_image_to_cluster "${OTEL_COLLECTOR_IMAGE}"
    load_image_to_cluster "${OTEL_LGTM_IMAGE}"
  fi
}

ensure_kind_cluster() {
  require_cmd kind
  local kubeconfig
  kubeconfig="$(k8s_kubeconfig_file)"
  mkdir -p "$(dirname "${kubeconfig}")"
  write_kind_cluster_config
  if ! kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
    kind create cluster \
      --name "${K8S_CLUSTER_NAME}" \
      --image "${KIND_NODE_IMAGE}" \
      --config "$(kind_cluster_config_file)" \
      --kubeconfig "${kubeconfig}"
    connect_registry_to_kind_network || true
  else
    kind export kubeconfig --name "${K8S_CLUSTER_NAME}" --kubeconfig "${kubeconfig}" >/dev/null
    connect_registry_to_kind_network || true
  fi
}

write_kind_cluster_config() {
  local config_file base_config
  config_file="$(kind_cluster_config_file)"
  base_config="$(kind_cluster_base_config_file)"
  mkdir -p "$(dirname "${config_file}")"
  {
    printf 'kind: Cluster\n'
    printf 'apiVersion: kind.x-k8s.io/v1alpha4\n'
    printf 'containerdConfigPatches:\n'
    printf '  - |-\n'
    printf '    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]\n' "${LOCAL_REGISTRY_HOST}"
    printf '      endpoint = ["http://%s:5000"]\n' "${LOCAL_REGISTRY_NAME}"
    awk 'found { print } /^nodes:/ { found = 1; print }' "${base_config}"
  } >"${config_file}"
}

registry_container_running() {
  docker inspect -f '{{.State.Running}}' "${LOCAL_REGISTRY_NAME}" 2>/dev/null | grep -Fxq true
}

registry_http_ready() {
  curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${LOCAL_REGISTRY_PORT}/v2/" >/dev/null 2>&1
}

connect_registry_to_kind_network() {
  if ! docker network inspect kind >/dev/null 2>&1; then
    return 0
  fi
  if ! docker inspect "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1; then
    return 0
  fi
  if docker inspect -f '{{json .NetworkSettings.Networks}}' "${LOCAL_REGISTRY_NAME}" | grep -q '"kind"'; then
    return 0
  fi
  docker network connect kind "${LOCAL_REGISTRY_NAME}" >/dev/null
}

registry_kind_mirror_configured() {
  local node
  node="${K8S_CLUSTER_NAME}-control-plane"
  docker exec "${node}" grep -Fq "registry.mirrors.\"${LOCAL_REGISTRY_HOST}\"" /etc/containerd/config.toml >/dev/null 2>&1 &&
    docker exec "${node}" grep -Fq "endpoint = [\"http://${LOCAL_REGISTRY_NAME}:5000\"]" /etc/containerd/config.toml >/dev/null 2>&1
}

kind_nodeports_ready() {
  if [ "${K8S_ENV_NAME}" != "kind" ]; then
    return 1
  fi
  curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1 &&
    curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_GATEWAY_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1 &&
    { [ "${OTEL:-1}" != "1" ] && [ "${OTEL:-1}" != "true" ] || curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_LGTM_LOCAL_UI_PORT}/api/health" >/dev/null 2>&1; }
}

wait_for_kind_nodeports_ready() {
  local deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if kind_nodeports_ready; then
      return 0
    fi
    sleep 2
  done
  return 1
}

ensure_k8s_local_access() {
  if [ "${K8S_ENV_NAME}" = "kind" ]; then
    if wait_for_kind_nodeports_ready; then
      return 0
    fi
    echo "kind NodePort access is not ready; recreate the kind cluster with make kind-reset && make kind-up" >&2
    return 1
  fi
  if curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1 &&
    curl --connect-timeout 2 --max-time 5 -fsS "http://127.0.0.1:${K8S_GATEWAY_LOCAL_HTTP_PORT}/healthz" >/dev/null 2>&1; then
    return 0
  fi
  echo "k8s local access is not ready; expose controld and gatewayd on the configured local ports before running local checks" >&2
  return 1
}

emit_k8s_proxy_status() {
  local proxy_config_json="$1"
  if [ -z "${proxy_config_json}" ]; then
    return 0
  fi
  printf '%s' "${proxy_config_json}" | python3 -c '
import json
import sys

try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(0)

def dedupe_csv(raw: str) -> str:
    values = []
    seen = set()
    for item in raw.split(","):
        value = item.strip()
        if not value or value in seen:
            continue
        seen.add(value)
        values.append(value)
    return ",".join(values)

data = payload.get("data") or {}
for key in ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REGISTRY_PROXY_URL", "REGISTRY_NO_PROXY"):
    value = data.get(key, "")
    if key in {"NO_PROXY", "REGISTRY_NO_PROXY"}:
        value = dedupe_csv(value)
    print(f"{key.lower()}={value}")
'
}

emit_node_summary_status() {
  local nodesz_url="$1"
  local body=""
  body="$(curl --noproxy 'localhost,127.0.0.1,::1' --connect-timeout 2 --max-time 5 -fsS "${nodesz_url}" 2>/dev/null || true)"
  if [ -z "${body}" ]; then
    echo "node_count=0"
    echo "node_summary_fresh=false"
    echo "axnoded_ready=false"
    echo "interface_pool=0/0/0"
    echo "cgroup_pool=0/0/0"
    echo "running_allocation_ids=0"
    echo "active_allocation_ids=0"
    echo "running_containers=0"
    echo "mounted_images=0"
    echo "imagemgr_ready_nodes=0"
    echo "imagefsd_ready_nodes=0"
    echo "volumed_ready_nodes=0"
    echo "volumed_error_nodes=0"
    return 0
  fi
  printf '%s' "${body}" | python3 -c '
import json
import sys

try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(1)

nodes = payload.get("nodes", [])
node_count = len(nodes)
fresh_nodes = 0
summary_fresh_nodes = 0
axnoded_ready_nodes = 0
interface_using = interface_idle = interface_capacity = 0
cgroup_using = cgroup_idle = cgroup_capacity = 0
runtime_slots_using = runtime_slots_idle = runtime_slots_capacity = runtime_slots_unavailable = 0
running_allocation_ids = 0
active_allocation_ids = 0
running_containers = 0
mounted_images = 0
imagemgr_ready_nodes = 0
imagefsd_ready_nodes = 0
volumed_ready_nodes = 0
volumed_error_nodes = 0

for node in nodes:
    if node.get("fresh"):
        fresh_nodes += 1
    if node.get("summary_fresh"):
        summary_fresh_nodes += 1
    summary = node.get("summary") or {}
    components = summary.get("components") or {}
    axnoded = components.get("axnoded") or {}
    imagemgr = components.get("imagemgr") or {}
    imagefsd = components.get("imagefsd") or {}
    volumed = components.get("volumed") or {}
    if axnoded.get("ready") and axnoded.get("state") == 1:
        axnoded_ready_nodes += 1
    if imagemgr.get("reachable") and imagemgr.get("state") == 1:
        imagemgr_ready_nodes += 1
    if imagefsd.get("reachable") and imagefsd.get("state") == 1:
        imagefsd_ready_nodes += 1
    if volumed.get("reachable") and volumed.get("state") == 1:
        volumed_ready_nodes += 1
    if volumed.get("state") == 4:
        volumed_error_nodes += 1
    pools = summary.get("pools") or {}
    interface_pool = pools.get("interface") or {}
    cgroup_pool = pools.get("cgroup") or {}
    runtime_slots = pools.get("runtime_slots") or {}
    interface_using += int(interface_pool.get("using") or 0)
    interface_idle += int(interface_pool.get("idle") or 0)
    interface_capacity += int(interface_pool.get("capacity") or 0)
    cgroup_using += int(cgroup_pool.get("using") or 0)
    cgroup_idle += int(cgroup_pool.get("idle") or 0)
    cgroup_capacity += int(cgroup_pool.get("capacity") or 0)
    runtime_slots_using += int(runtime_slots.get("using") or 0)
    runtime_slots_idle += int(runtime_slots.get("idle") or 0)
    runtime_slots_capacity += int(runtime_slots.get("capacity") or 0)
    runtime_slots_unavailable += int(runtime_slots.get("unavailable") or 0)
    running_allocation_ids += len(axnoded.get("running_allocation_ids") or [])
    active_allocation_ids += len(axnoded.get("active_allocation_ids") or [])
    running_containers += int(axnoded.get("running_containers") or 0)
    mounted_images += int(imagemgr.get("mounted_image_count") or 0)

print(f"node_count={node_count}")
print(f"fresh_nodes={fresh_nodes}")
print(f"summary_fresh_nodes={summary_fresh_nodes}")
print("node_summary_fresh=true" if node_count > 0 and summary_fresh_nodes == node_count else "node_summary_fresh=false")
print(f"axnoded_ready_nodes={axnoded_ready_nodes}")
print("axnoded_ready=true" if node_count > 0 and axnoded_ready_nodes == node_count else "axnoded_ready=false")
print(f"interface_pool={interface_using}/{interface_idle}/{interface_capacity}")
print(f"cgroup_pool={cgroup_using}/{cgroup_idle}/{cgroup_capacity}")
print(f"runtime_slots={runtime_slots_using}/{runtime_slots_idle}/{runtime_slots_capacity}/{runtime_slots_unavailable}")
print(f"running_allocation_ids={running_allocation_ids}")
print(f"active_allocation_ids={active_allocation_ids}")
print(f"running_containers={running_containers}")
print(f"mounted_images={mounted_images}")
print(f"imagemgr_ready_nodes={imagemgr_ready_nodes}")
print(f"imagefsd_ready_nodes={imagefsd_ready_nodes}")
print(f"volumed_ready_nodes={volumed_ready_nodes}")
print(f"volumed_error_nodes={volumed_error_nodes}")
'
}

local_smoke_wait_for_compose_allocation_cleanup() {
  local timeout_seconds="${1:-120}"
  case "${timeout_seconds}" in
    '' | *[!0-9]*) echo "cleanup timeout must be a positive integer" >&2; return 1 ;;
    0) echo "cleanup timeout must be positive" >&2; return 1 ;;
  esac
  local deadline=$((SECONDS + timeout_seconds))
  local status_body=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    status_body="$(bash "${AXERN_ROOT}/scripts/dev-env/compose-status.sh" 2>/dev/null || true)"
    if printf '%s\n' "${status_body}" | grep -q '^controld_health=ready$' &&
      printf '%s\n' "${status_body}" | grep -q '^node_summary_fresh=true$' &&
      printf '%s\n' "${status_body}" | grep -q '^axnoded_ready=true$' &&
      printf '%s\n' "${status_body}" | grep -Eq '^interface_pool=0/[0-9]+/[0-9]+$' &&
      printf '%s\n' "${status_body}" | grep -Eq '^cgroup_pool=0/[0-9]+/[0-9]+$' &&
      printf '%s\n' "${status_body}" | grep -Eq '^runtime_slots=0/[0-9]+/[0-9]+/[0-9]+$' &&
      printf '%s\n' "${status_body}" | grep -q '^running_allocation_ids=0$' &&
      printf '%s\n' "${status_body}" | grep -q '^active_allocation_ids=0$' &&
      printf '%s\n' "${status_body}" | grep -q '^running_containers=0$'; then
      echo "compose_allocation_cleanup_converged=true"
      return 0
    fi
    sleep 1
  done
  echo "compose allocation cleanup did not converge within ${timeout_seconds}s" >&2
  printf '%s\n' "${status_body}" >&2
  return 1
}

emit_compose_imported_image_status() {
  local node_container="${COMPOSE_PROJECT_NAME}-node-1"
  local body=""
  if docker ps --format '{{.Names}}' | grep -Fxq "${node_container}"; then
    body="$(docker exec "${node_container}" curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null || true)"
  fi
  emit_imported_image_count "${body}"
}

emit_kind_imported_image_status() {
  local pods
  pods="$(kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)"
  local total=0
  while IFS= read -r pod; do
    [ -n "${pod}" ] || continue
    local body count
    body="$(kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null || true)"
    count="$(printf '%s' "${body}" | python3 -c 'import json,sys
try:
    payload=json.load(sys.stdin)
except Exception:
    print(0)
else:
    print(len(payload.get("imported_images") or []))
')"
    total=$((total + count))
  done <<< "${pods}"
  echo "imported_images=${total}"
}

emit_registry_status() {
  if ! docker inspect "${LOCAL_REGISTRY_NAME}" >/dev/null 2>&1; then
    echo "registry=absent"
    echo "registry_host=${LOCAL_REGISTRY_HOST}"
    echo "registry_cluster_host=${LOCAL_REGISTRY_CLUSTER_HOST}"
    echo "registry_kind_mirror=unknown"
    return 0
  fi
  if registry_container_running; then
    echo "registry=running"
  else
    echo "registry=stopped"
  fi
  echo "registry_host=${LOCAL_REGISTRY_HOST}"
  echo "registry_cluster_host=${LOCAL_REGISTRY_CLUSTER_HOST}"
  if registry_http_ready; then
    echo "registry_http=ready"
  else
    echo "registry_http=unreachable"
  fi
  if docker inspect -f '{{json .NetworkSettings.Networks}}' "${LOCAL_REGISTRY_NAME}" 2>/dev/null | grep -q '"kind"'; then
    echo "registry_kind_network=kind"
  else
    echo "registry_kind_network=missing"
  fi
  if kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
    if registry_kind_mirror_configured; then
      echo "registry_kind_mirror=configured"
    else
      echo "registry_kind_mirror=missing"
      echo "registry_kind_mirror_hint=run make kind-reset to recreate the cluster with the local registry mirror"
    fi
  else
    echo "registry_kind_mirror=cluster_absent"
  fi
}

emit_imported_image_count() {
  local body="$1"
  if [ -z "${body}" ]; then
    echo "imported_images=0"
    return 0
  fi
  printf '%s' "${body}" | python3 -c '
import json
import sys
try:
    payload = json.load(sys.stdin)
except Exception:
    print("imported_images=0")
else:
    key = "imported_images"
    print(f"imported_images={len(payload.get(key) or [])}")
'
}
