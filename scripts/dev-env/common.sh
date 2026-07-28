#!/usr/bin/env bash

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY_ROOT="${AXERN_ROOT}/deploy/local"
STATE_ROOT="${DEPLOY_ROOT}/state"
LOCKS_STATE_DIR="${STATE_ROOT}/locks"
COMPOSE_STATE_DIR="${STATE_ROOT}/compose"
K8S_ENV_NAME="${K8S_ENV_NAME:-k8s}"
K8S_STATE_DIR="${STATE_ROOT}/${K8S_ENV_NAME}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-axern-local}"
K8S_NAMESPACE="${K8S_NAMESPACE:-axern-local}"
K8S_CLUSTER_NAME="${K8S_CLUSTER_NAME:-axern-local}"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.31.2}"
LOCAL_REGISTRY_NAME="${LOCAL_REGISTRY_NAME:-axern-registry}"
LOCAL_REGISTRY_PORT="${LOCAL_REGISTRY_PORT:-5001}"
LOCAL_REGISTRY_IMAGE="${LOCAL_REGISTRY_IMAGE:-registry:2}"
LOCAL_REGISTRY_HOST="${LOCAL_REGISTRY_HOST:-localhost:${LOCAL_REGISTRY_PORT}}"
LOCAL_REGISTRY_CLUSTER_HOST="${LOCAL_REGISTRY_CLUSTER_HOST:-host.docker.internal:${LOCAL_REGISTRY_PORT}}"
NYDUS_BUILDER_IMAGE="${NYDUS_BUILDER_IMAGE:-axern/nydus-builder:dev}"
NYDUS_SOURCE_IMAGE="${NYDUS_SOURCE_IMAGE:-axern/python311-runtime:dev}"
NYDUS_SOURCE_REGISTRY_IMAGE="${NYDUS_SOURCE_REGISTRY_IMAGE:-${LOCAL_REGISTRY_HOST}/axern/nydus-smoke-source:dev}"
NYDUS_LOCAL_IMAGE="${NYDUS_LOCAL_IMAGE:-${LOCAL_REGISTRY_HOST}/axern/nydus-smoke:dev}"
CONTROLD_IMAGE="${CONTROLD_IMAGE:-axern/local-controld:dev}"
TUNNELD_IMAGE="${TUNNELD_IMAGE:-axern/local-tunneld:dev}"
GATEWAYD_IMAGE="${GATEWAYD_IMAGE:-axern/local-gatewayd:dev}"
NODE_ALL_IN_ONE_IMAGE="${NODE_ALL_IN_ONE_IMAGE:-axern/local-node-all-in-one:dev}"
PYTHON311_RUNTIME_IMAGE="${PYTHON311_RUNTIME_IMAGE:-axern/python311-runtime:dev}"
SERVER_BASE_RUNTIME_IMAGE="${SERVER_BASE_RUNTIME_IMAGE:-axern/server-base-runtime:dev}"
CODING_BASE_RUNTIME_IMAGE="${CODING_BASE_RUNTIME_IMAGE:-axern/coding-base-runtime:dev}"
DESKTOP_BASE_RUNTIME_IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE:-axern/desktop-base-runtime:dev}"
CLAUDE_CODE_BUNDLE_IMAGE="${CLAUDE_CODE_BUNDLE_IMAGE:-axern/claude-code-bundle:dev}"
CODEX_BUNDLE_IMAGE="${CODEX_BUNDLE_IMAGE:-axern/codex-bundle:dev}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:16-alpine}"
MINIO_IMAGE="${MINIO_IMAGE:-minio/minio:RELEASE.2025-02-28T09-55-16Z}"
OTEL_COLLECTOR_IMAGE="${OTEL_COLLECTOR_IMAGE:-otel/opentelemetry-collector:0.150.1}"
OTEL_LGTM_IMAGE="${OTEL_LGTM_IMAGE:-grafana/otel-lgtm:0.11.16}"
VERIFY_IMAGE_TAG="${VERIFY_IMAGE_TAG:-axnoded-verify:latest}"
SECRETS_MASTER_KEY_FILE_NAME="secrets-master-key"
CLI_ENV_FILE_NAME="axern.env"
COMPOSE_ENV_FILE_NAME="compose.env"
K8S_KUBECONFIG_FILE="${K8S_STATE_DIR}/kubeconfig"
COMPOSE_CONTROLD_HTTP_PORT="${COMPOSE_CONTROLD_HTTP_PORT:-24101}"
COMPOSE_GATEWAY_CONTROL_PORT="${COMPOSE_GATEWAY_CONTROL_PORT:-25000}"
COMPOSE_GATEWAY_HTTP_PORT="${COMPOSE_GATEWAY_HTTP_PORT:-25080}"
COMPOSE_GATEWAY_SSH_PORT="${COMPOSE_GATEWAY_SSH_PORT:-25022}"
COMPOSE_POSTGRES_PORT="${COMPOSE_POSTGRES_PORT:-25432}"
COMPOSE_MINIO_API_PORT="${COMPOSE_MINIO_API_PORT:-29000}"
COMPOSE_MINIO_CONSOLE_PORT="${COMPOSE_MINIO_CONSOLE_PORT:-29001}"
COMPOSE_OTEL_GRPC_PORT="${COMPOSE_OTEL_GRPC_PORT:-4317}"
COMPOSE_OTEL_HTTP_PORT="${COMPOSE_OTEL_HTTP_PORT:-4318}"
COMPOSE_LGTM_UI_PORT="${COMPOSE_LGTM_UI_PORT:-13000}"

k8s_default_controld_local_http_port() {
  case "$1" in
    kind) printf '24211\n' ;;
    *) printf '24111\n' ;;
  esac
}

k8s_default_gateway_local_http_port() {
  case "$1" in
    kind) printf '25082\n' ;;
    *) printf '25081\n' ;;
  esac
}

k8s_default_gateway_local_control_port() {
  case "$1" in
    kind) printf '25002\n' ;;
    *) printf '25001\n' ;;
  esac
}

K8S_CONTROLD_LOCAL_HTTP_PORT="${K8S_CONTROLD_LOCAL_HTTP_PORT:-$(k8s_default_controld_local_http_port "${K8S_ENV_NAME}")}"
K8S_GATEWAY_LOCAL_CONTROL_PORT="${K8S_GATEWAY_LOCAL_CONTROL_PORT:-$(k8s_default_gateway_local_control_port "${K8S_ENV_NAME}")}"
K8S_GATEWAY_LOCAL_HTTP_PORT="${K8S_GATEWAY_LOCAL_HTTP_PORT:-$(k8s_default_gateway_local_http_port "${K8S_ENV_NAME}")}"
K8S_GATEWAY_LOCAL_SSH_PORT="${K8S_GATEWAY_LOCAL_SSH_PORT:-25023}"
K8S_OTEL_LOCAL_GRPC_PORT="${K8S_OTEL_LOCAL_GRPC_PORT:-24317}"
K8S_OTEL_LOCAL_HTTP_PORT="${K8S_OTEL_LOCAL_HTTP_PORT:-24318}"
K8S_LGTM_LOCAL_UI_PORT="${K8S_LGTM_LOCAL_UI_PORT:-13001}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

axern_go_bin() {
  if [ -n "${AXERN_GO_BIN:-}" ]; then
    printf '%s\n' "${AXERN_GO_BIN}"
    return
  fi
  if [ -n "${GO:-}" ]; then
    printf '%s\n' "${GO}"
    return
  fi
  if [ -n "${GOROOT:-}" ] && [ -x "${GOROOT}/bin/go" ]; then
    printf '%s\n' "${GOROOT}/bin/go"
    return
  fi
  command -v go
}

docker_image_resolved_digest_ref() {
  local requested_image_ref="$1"
  local digests_json
  digests_json="$(docker image inspect "${requested_image_ref}" --format '{{json .RepoDigests}}')" || return 1
  python3 - "${requested_image_ref}" "${digests_json}" <<'PY'
import json
import sys

requested_image_ref = sys.argv[1]
payload = sys.argv[2]

def repository(ref):
    base = ref.split("@", 1)[0]
    slash = base.rfind("/")
    colon = base.rfind(":")
    if colon > slash:
        base = base[:colon]
    return base

def normalize_repository(repo):
    first = repo.split("/", 1)[0]
    if "." in first or ":" in first or first == "localhost":
        return repo
    if "/" not in repo:
        return "index.docker.io/library/" + repo
    return "index.docker.io/" + repo

try:
    digests = json.loads(payload)
except json.JSONDecodeError:
    digests = []
if not digests:
    print(requested_image_ref)
    raise SystemExit(0)

repo = normalize_repository(repository(requested_image_ref))
for digest in digests:
    digest_repo, digest_value = digest.split("@", 1)
    if normalize_repository(digest_repo) == repo:
        print(repo + "@" + digest_value)
        raise SystemExit(0)
digest_repo, digest_value = digests[0].split("@", 1)
print(normalize_repository(digest_repo) + "@" + digest_value)
PY
}

linux_platform_from_uname_arch() {
  case "$1" in
    aarch64 | arm64) printf 'linux/arm64\n' ;;
    x86_64 | amd64) printf 'linux/amd64\n' ;;
    armv7l) printf 'linux/arm/v7\n' ;;
    armv6l) printf 'linux/arm/v6\n' ;;
    *) return 1 ;;
  esac
}

docker_image_resolved_digest_ref_for_platform() {
  local requested_image_ref="$1"
  local platform="$2"
  local raw
  if ! raw="$(docker buildx imagetools inspect --raw "${requested_image_ref}" 2>/dev/null)"; then
    docker_image_resolved_digest_ref "${requested_image_ref}"
    return 0
  fi
  python3 - "${requested_image_ref}" "${platform}" "${raw}" <<'PY'
import json
import sys

requested_image_ref = sys.argv[1]
platform = sys.argv[2]
payload = sys.argv[3]

def repository(ref):
    base = ref.split("@", 1)[0]
    slash = base.rfind("/")
    colon = base.rfind(":")
    if colon > slash:
        base = base[:colon]
    return base

def normalize_repository(repo):
    first = repo.split("/", 1)[0]
    if "." in first or ":" in first or first == "localhost":
        return repo
    if "/" not in repo:
        return "index.docker.io/library/" + repo
    return "index.docker.io/" + repo

def split_platform(value):
    parts = value.split("/")
    os = parts[0] if len(parts) > 0 else ""
    arch = parts[1] if len(parts) > 1 else ""
    variant = parts[2] if len(parts) > 2 else ""
    return os, arch, variant

repo = normalize_repository(repository(requested_image_ref))
wanted_os, wanted_arch, wanted_variant = split_platform(platform)
try:
    doc = json.loads(payload)
except json.JSONDecodeError:
    print(requested_image_ref)
    raise SystemExit(0)

media_type = doc.get("mediaType", "")
if media_type.endswith(".image.manifest.v1+json") or media_type.endswith(".image.manifest.v2+json"):
    if "@sha256:" in requested_image_ref:
        print(repo + "@" + requested_image_ref.split("@", 1)[1])
    else:
        print(requested_image_ref)
    raise SystemExit(0)

for manifest in doc.get("manifests", []):
    item_platform = manifest.get("platform") or {}
    if item_platform.get("os") != wanted_os or item_platform.get("architecture") != wanted_arch:
        continue
    item_variant = item_platform.get("variant", "")
    if wanted_variant and item_variant != wanted_variant:
        continue
    digest = manifest.get("digest", "")
    if digest:
        print(repo + "@" + digest)
        raise SystemExit(0)

print(requested_image_ref)
PY
}

ensure_docker_image_resolved_for_platform() {
  local requested_image_ref="$1"
  local platform="$2"
  local resolved_image_ref
  resolved_image_ref="$(docker_image_resolved_digest_ref_for_platform "${requested_image_ref}" "${platform}")" || return 1
  if ! docker image inspect "${resolved_image_ref}" >/dev/null 2>&1; then
    docker pull --platform "${platform}" "${resolved_image_ref}" >/dev/null
  fi
  if ! docker_image_matches_platform "${resolved_image_ref}" "${platform}"; then
    echo "Docker image ${resolved_image_ref} does not match requested platform ${platform}" >&2
    return 1
  fi
  printf '%s\n' "${resolved_image_ref}"
}

docker_image_matches_platform() {
  local image_ref="$1"
  local platform="$2"
  local image_platform
  image_platform="$(docker image inspect "${image_ref}" --format '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}')" || return 1
  python3 - "${image_platform}" "${platform}" <<'PY'
import sys

actual = sys.argv[1].split("/")
wanted = sys.argv[2].split("/")

if len(actual) < 2 or len(wanted) < 2:
    raise SystemExit(1)
if actual[0] != wanted[0] or actual[1] != wanted[1]:
    raise SystemExit(1)
if len(wanted) > 2 and (len(actual) <= 2 or actual[2] != wanted[2]):
    raise SystemExit(1)
PY
}

ensure_state_dirs() {
  mkdir -p \
    "${LOCKS_STATE_DIR}" \
    "${COMPOSE_STATE_DIR}/certs" \
    "${COMPOSE_STATE_DIR}/ssh" \
    "${COMPOSE_STATE_DIR}/postgres" \
    "${COMPOSE_STATE_DIR}/minio" \
    "${COMPOSE_STATE_DIR}/run" \
    "${COMPOSE_STATE_DIR}/logs" \
    "${K8S_STATE_DIR}/certs" \
    "${K8S_STATE_DIR}/ssh" \
    "${K8S_STATE_DIR}/logs"
}

lock_dir_for() {
  local lock_name="$1"
  printf '%s/%s.lock\n' "${LOCKS_STATE_DIR}" "${lock_name}"
}

lock_owner_file() {
  local lock_name="$1"
  printf '%s/owner\n' "$(lock_dir_for "${lock_name}")"
}

lock_now_epoch() {
  date +%s
}

format_duration_compact() {
  local seconds="${1:-0}"
  if [ "${seconds}" -lt 60 ]; then
    printf '%ss\n' "${seconds}"
    return 0
  fi
  if [ "${seconds}" -lt 3600 ]; then
    printf '%sm\n' "$((seconds / 60))"
    return 0
  fi
  if [ "${seconds}" -lt 86400 ]; then
    printf '%sh\n' "$((seconds / 3600))"
    return 0
  fi
  printf '%sd\n' "$((seconds / 86400))"
}

read_lock_metadata() {
  local lock_name="$1"
  local owner_file
  owner_file="$(lock_owner_file "${lock_name}")"
  LOCK_META_PID=""
  LOCK_META_SCRIPT=""
  LOCK_META_STARTED_AT=""
  LOCK_META_EPOCH=""
  if [ ! -f "${owner_file}" ]; then
    return 1
  fi
  while IFS='=' read -r key value; do
    case "${key}" in
      pid) LOCK_META_PID="${value}" ;;
      script) LOCK_META_SCRIPT="${value}" ;;
      started_at) LOCK_META_STARTED_AT="${value}" ;;
      epoch) LOCK_META_EPOCH="${value}" ;;
    esac
  done < "${owner_file}"
  return 0
}

lock_is_stale() {
  local lock_name="$1"
  if ! read_lock_metadata "${lock_name}"; then
    return 1
  fi
  if [ -z "${LOCK_META_PID}" ]; then
    return 0
  fi
  if kill -0 "${LOCK_META_PID}" >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

reclaim_stale_lock() {
  local lock_name="$1"
  if lock_is_stale "${lock_name}"; then
    rm -rf "$(lock_dir_for "${lock_name}")"
    return 0
  fi
  return 1
}

describe_lock_holder() {
  local lock_name="$1"
  if ! read_lock_metadata "${lock_name}"; then
    printf 'unknown\n'
    return 0
  fi
  local age="unknown"
  if [ -n "${LOCK_META_EPOCH}" ]; then
    local now
    now="$(lock_now_epoch)"
    if [ "${now}" -ge "${LOCK_META_EPOCH}" ] 2>/dev/null; then
      age="$(format_duration_compact "$((now - LOCK_META_EPOCH))")"
    fi
  fi
  printf '%s pid=%s age=%s\n' "${LOCK_META_SCRIPT:-unknown}" "${LOCK_META_PID:-unknown}" "${age}"
}

emit_lock_status() {
  local lock_name="$1"
  if [ ! -d "$(lock_dir_for "${lock_name}")" ]; then
    echo "lock_state=unlocked"
    return 0
  fi
  if lock_is_stale "${lock_name}"; then
    echo "lock_state=unlocked"
    return 0
  fi
  echo "lock_state=locked"
  if read_lock_metadata "${lock_name}"; then
    echo "lock_script=${LOCK_META_SCRIPT:-unknown}"
    echo "lock_pid=${LOCK_META_PID:-unknown}"
    local age="unknown"
    if [ -n "${LOCK_META_EPOCH}" ]; then
      local now
      now="$(lock_now_epoch)"
      if [ "${now}" -ge "${LOCK_META_EPOCH}" ] 2>/dev/null; then
        age="$(format_duration_compact "$((now - LOCK_META_EPOCH))")"
      fi
    fi
    echo "lock_age=${age}"
  fi
}

begin_named_lock() {
  local lock_name="$1"
  if [ "${AXERN_DEV_LOCK_HELD:-}" = "${lock_name}" ]; then
    export AXERN_DEV_LOCK_OWNED=0
    return 0
  fi
  ensure_state_dirs
  reclaim_stale_lock "${lock_name}" >/dev/null 2>&1 || true
  local lock_dir
  lock_dir="$(lock_dir_for "${lock_name}")"
  if ! mkdir "${lock_dir}" 2>/dev/null; then
    echo "environment busy: ${lock_name} lock held by $(describe_lock_holder "${lock_name}")" >&2
    return 1
  fi
  local owner_file started_at epoch
  owner_file="$(lock_owner_file "${lock_name}")"
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  epoch="$(lock_now_epoch)"
  cat > "${owner_file}" <<EOF
pid=$$
script=$(basename "$0")
started_at=${started_at}
epoch=${epoch}
EOF
  export AXERN_DEV_LOCK_HELD="${lock_name}"
  export AXERN_DEV_LOCK_OWNED=1
}

end_named_lock() {
  local lock_name="$1"
  if [ "${AXERN_DEV_LOCK_HELD:-}" != "${lock_name}" ]; then
    return 0
  fi
  if [ "${AXERN_DEV_LOCK_OWNED:-0}" != "1" ]; then
    return 0
  fi
  rm -rf "$(lock_dir_for "${lock_name}")"
  unset AXERN_DEV_LOCK_HELD
  unset AXERN_DEV_LOCK_OWNED
}

begin_env_lock() {
  begin_named_lock "env-$1"
}

end_env_lock() {
  end_named_lock "env-$1"
}

secrets_master_key_file() {
  local env_name="$1"
  printf '%s/%s\n' "${STATE_ROOT}/${env_name}" "${SECRETS_MASTER_KEY_FILE_NAME}"
}

cli_env_file() {
  local env_name="$1"
  printf '%s/%s\n' "${STATE_ROOT}/${env_name}" "${CLI_ENV_FILE_NAME}"
}

axern_config_file() {
  if [ -n "${AXERN_CONFIG:-}" ]; then
    printf '%s\n' "${AXERN_CONFIG}"
    return 0
  fi
  printf '%s/.config/axern/config.json\n' "${HOME}"
}

compose_env_file() {
  printf '%s/%s\n' "${COMPOSE_STATE_DIR}" "${COMPOSE_ENV_FILE_NAME}"
}

k8s_kubeconfig_file() {
  printf '%s\n' "${K8S_KUBECONFIG_FILE}"
}

kind_cluster_config_file() {
  printf '%s/cluster.yaml\n' "$(dirname "$(k8s_kubeconfig_file)")"
}

kind_cluster_base_config_file() {
  printf '%s/kind/cluster.yaml\n' "${DEPLOY_ROOT}"
}

with_kubeconfig() {
  if [ -n "${KUBECONFIG:-}" ]; then
    printf '%s\n' "${KUBECONFIG}"
  else
    printf '%s\n' "$(k8s_kubeconfig_file)"
  fi
}

wait_for_http_ready() {
  local url="$1"
  local timeout="${2:-90}"
  local label="${3:-http}"
  local started_at=${SECONDS}
  local next_report=$((SECONDS + 10))
  local deadline=$((SECONDS + timeout))
  local curl_status=0
  while true; do
    if curl --noproxy 'localhost,127.0.0.1,::1' --connect-timeout 2 --max-time 5 -fsS "${url}" >/dev/null 2>&1; then
      return 0
    else
      curl_status=$?
    fi
    if [ "${SECONDS}" -ge "${deadline}" ]; then
      echo "wait_ready_timeout=${label} elapsed_seconds=$((SECONDS - started_at)) timeout_seconds=${timeout} last_curl_exit_code=${curl_status}" >&2
      return 1
    fi
    if [ "${SECONDS}" -ge "${next_report}" ]; then
      echo "wait_ready_pending=${label} elapsed_seconds=$((SECONDS - started_at)) timeout_seconds=${timeout}"
      next_report=$((SECONDS + 10))
    fi
    sleep 1
  done
}
