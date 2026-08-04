#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

task_base_image="${LOCAL_CODEX_IMAGE_MOUNT_SMOKE_TASK_BASE_IMAGE:-${CODING_BASE_RUNTIME_IMAGE}}"
bundle_source_image="${LOCAL_CODEX_IMAGE_MOUNT_SMOKE_BUNDLE_IMAGE:-${CODEX_BUNDLE_IMAGE}}"

if ! docker image inspect "${task_base_image}" >/dev/null 2>&1; then
  echo "missing task smoke base image ${task_base_image}; run make local-images-build first" >&2
  exit 1
fi

IMAGE_REF="${bundle_source_image}" \
  bash "${AXERN_ROOT}/runtime/axnoded/scripts/runtime/build-codex-bundle-image.sh" >/dev/null

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -Fxq "${node_container}"; then
  echo "compose node container is not running: ${node_container}" >&2
  exit 1
fi

build_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-codex-image-mount-smoke.XXXXXX")"
cleanup_build_dir() {
  rm -rf "${build_dir}"
}
trap 'cleanup_build_dir; end_env_lock compose' EXIT

cat >"${build_dir}/Task.Dockerfile" <<'EOF'
ARG TASK_BASE_IMAGE
FROM ${TASK_BASE_IMAGE}
RUN mkdir -p /opt/axern/agents/codex
EOF

task_source_image="axern/codex-image-mount-smoke-task:local"
docker build -q --build-arg "TASK_BASE_IMAGE=${task_base_image}" -f "${build_dir}/Task.Dockerfile" -t "${task_source_image}" "${build_dir}" >/dev/null

task_push_output="$(IMAGE="${task_source_image}" bash "${AXERN_ROOT}/scripts/dev-env/registry-image-push.sh")"
task_cluster_image="$(printf '%s\n' "${task_push_output}" | awk -F= '$1 == "cluster_image" {print $2}')"
if [ -z "${task_cluster_image}" ]; then
  printf '%s\n' "${task_push_output}" >&2
  echo "task image registry push did not return cluster_image" >&2
  exit 1
fi

bundle_push_output="$(IMAGE="${bundle_source_image}" bash "${AXERN_ROOT}/scripts/dev-env/registry-image-push.sh")"
bundle_cluster_image="$(printf '%s\n' "${bundle_push_output}" | awk -F= '$1 == "cluster_image" {print $2}')"
if [ -z "${bundle_cluster_image}" ]; then
  printf '%s\n' "${bundle_push_output}" >&2
  echo "bundle image registry push did not return cluster_image" >&2
  exit 1
fi

namespace="compose-codex-image-mount-smoke-$(date +%s)"
run_id=""
environment_id=""
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

extract_run_json_field() {
  local field="$1"
  local text
  text="$(cat)"
  python3 -c '
import json
import sys

field = sys.argv[1]
text = sys.argv[2].lstrip()
obj, _ = json.JSONDecoder().raw_decode(text)
print(obj["run"].get(field, ""))
' "${field}" "${text}"
}

dump_codex_image_mount_smoke_failure() {
  if [ -n "${run_id}" ]; then
    echo "--- run get ${run_id} ---" >&2
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run get -o json "${run_id}" 2>/dev/null | python3 -m json.tool >&2 || true
  fi
  echo "--- compose ps ---" >&2
  docker compose --project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${DEPLOY_ROOT}/compose/docker-compose.yml" ps >&2 || true
  echo "--- ${node_container}: imagemgr log tail ---" >&2
  docker exec "${node_container}" tail -n 160 /var/lib/imagemgr/logs/imagemgr.log >&2 || true
  echo "--- ${node_container}: axnoded log tail ---" >&2
  docker exec "${node_container}" tail -n 160 /var/log/axnoded/axnoded.log >&2 || true
  echo "--- ${node_container}: image mount inventory ---" >&2
  docker exec "${node_container}" curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null | python3 -m json.tool >&2 || true
}

cleanup_codex_image_mount_smoke() {
  local rc=$?
  if [ "${rc}" -ne 0 ]; then
    dump_codex_image_mount_smoke_failure || true
  fi
  if [ -n "${run_id}" ]; then
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json >/dev/null 2>&1 || true
    local_smoke_wait_for_run_terminal "${run_id}" >/dev/null 2>&1 || true
  fi
  if [ -n "${environment_id}" ]; then
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null 2>&1 || true
    environment_id=""
  fi
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null 2>&1 || true
  cleanup_build_dir
  end_env_lock compose
  return "${rc}"
}
trap cleanup_codex_image_mount_smoke EXIT

create_output="$(
  local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" run --detach -o json \
      --namespace "${namespace}" \
      --image-mount "${bundle_cluster_image}:/opt/axern/agents/codex:ro" \
      "${task_cluster_image}" -- /bin/sh -lc \
      'test -r /opt/axern/agents/codex/opt/axern/agent-bundle/manifest.json && LD_LIBRARY_PATH=/definitely-not-a-system-library /opt/axern/agents/codex/bin/codex --version | tee /tmp/codex-version.txt && grep -F "codex-cli" /tmp/codex-version.txt && ! touch /opt/axern/agents/codex/write-test && /bin/sh -c "printf sandbox-ok" && printf "\nagent-bundle-smoke-ready\n" && sleep 600' || true
)"
if ! run_id="$(extract_run_json_field id <<<"${create_output}" 2>/dev/null)"; then
  printf '%s\n' "${create_output}" >&2
  echo "failed to parse created run id" >&2
  exit 1
fi
if ! run_json="$(local_smoke_wait_for_run_status "${run_id}" running)"; then
  printf '%s\n' "${run_json}" >&2
  echo "run ${run_id} did not reach running" >&2
  exit 1
fi
run_id="$(extract_run_json_field id <<<"${run_json}")"
environment_id="$(extract_run_json_field environment_id <<<"${run_json}")"
if ! local_smoke_wait_for_run_output "${run_id}" agent-bundle-smoke-ready >/dev/null; then
  echo "run ${run_id} did not produce the bundle success marker" >&2
  exit 1
fi
bundle_mount_duration="$(local_smoke_imagemgr_mount_duration "${node_container}" "${bundle_cluster_image}")"
local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json >/dev/null
local_smoke_wait_for_run_terminal "${run_id}" >/dev/null
run_id=""
if [ -n "${environment_id}" ]; then
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null
  environment_id=""
fi
local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null
local_smoke_wait_for_image_mount_release "${node_container}" "${bundle_cluster_image}"

echo "codex_image_mount_smoke_ok=true"
echo "task_base_image=${task_base_image}"
echo "task_cluster_image=${task_cluster_image}"
echo "codex_bundle_source_image=${bundle_source_image}"
echo "bundle_cluster_image=${bundle_cluster_image}"
echo "bundle_mount_duration=${bundle_mount_duration}"
