#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

task_base_image="${LOCAL_CLAUDE_CODE_IMAGE_MOUNT_SMOKE_TASK_BASE_IMAGE:-${CODING_BASE_RUNTIME_IMAGE}}"
bundle_source_image="${LOCAL_CLAUDE_CODE_IMAGE_MOUNT_SMOKE_BUNDLE_IMAGE:-${CLAUDE_CODE_BUNDLE_IMAGE}}"

if ! docker image inspect "${task_base_image}" >/dev/null 2>&1; then
  echo "missing task smoke base image ${task_base_image}; run make local-images-build first" >&2
  exit 1
fi

IMAGE_REF="${bundle_source_image}" \
  bash "${AXERN_ROOT}/runtime/axnoded/scripts/runtime/build-claude-code-bundle-image.sh" >/dev/null

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -Fxq "${node_container}"; then
  echo "compose node container is not running: ${node_container}" >&2
  exit 1
fi

build_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-claude-code-image-mount-smoke.XXXXXX")"
cleanup_build_dir() {
  rm -rf "${build_dir}"
}
trap 'cleanup_build_dir; end_env_lock compose' EXIT

cat >"${build_dir}/Task.Dockerfile" <<'EOF'
ARG TASK_BASE_IMAGE
FROM ${TASK_BASE_IMAGE}
RUN mkdir -p /opt/axern/agents/claude-code
EOF

task_source_image="axern/claude-code-image-mount-smoke-task:local"
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

namespace="compose-claude-code-image-mount-smoke-$(date +%s)"
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

dump_claude_code_image_mount_smoke_failure() {
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

cleanup_claude_code_image_mount_smoke() {
  local rc=$?
  if [ "${rc}" -ne 0 ]; then
    dump_claude_code_image_mount_smoke_failure || true
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
trap cleanup_claude_code_image_mount_smoke EXIT

create_output="$(
  local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" run create -o json --wait --wait-for terminal --wait-timeout 180s \
      --namespace "${namespace}" \
      --image-ref "${task_cluster_image}" \
      --image-mount "${bundle_cluster_image}:/opt/axern/agents/claude-code:ro" \
      --argv /bin/sh --argv -lc \
      --argv '/opt/axern/agents/claude-code/bin/claude --version | tee /tmp/claude-version.txt && grep -F "Claude Code" /tmp/claude-version.txt && ! touch /opt/axern/agents/claude-code/write-test' || true
)"
if ! run_id="$(extract_run_json_field id <<<"${create_output}" 2>/dev/null)"; then
  printf '%s\n' "${create_output}" >&2
  echo "failed to parse created run id" >&2
  exit 1
fi
if ! run_json="$(local_smoke_wait_for_run_terminal "${run_id}")"; then
  printf '%s\n' "${run_json}" >&2
  echo "run ${run_id} did not reach a terminal status" >&2
  exit 1
fi
run_id="$(extract_run_json_field id <<<"${run_json}")"
environment_id="$(extract_run_json_field environment_id <<<"${run_json}")"
python3 -c 'import json,sys
run=json.load(sys.stdin)["run"]
if run["status"] != "succeeded":
    raise SystemExit("run status = %s, want succeeded" % run["status"])
if run.get("exit_code_known") and run.get("exit_code") != 0:
    raise SystemExit("run exit_code = %s, want 0" % run.get("exit_code"))
' <<<"${run_json}"
run_id=""
if [ -n "${environment_id}" ]; then
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null
  environment_id=""
fi
local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null

echo "claude_code_image_mount_smoke_ok=true"
echo "task_base_image=${task_base_image}"
echo "task_cluster_image=${task_cluster_image}"
echo "claude_runtime_image=${claude_runtime_image}"
echo "bundle_cluster_image=${bundle_cluster_image}"
