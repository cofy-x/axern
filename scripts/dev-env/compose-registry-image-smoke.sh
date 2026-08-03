#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd python3

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

source_image="${LOCAL_REGISTRY_IMAGE_SMOKE_SOURCE_IMAGE:-${PYTHON311_RUNTIME_IMAGE}}"
if ! docker image inspect "${source_image}" >/dev/null 2>&1; then
  echo "missing smoke source image ${source_image}; run make local-images-build first" >&2
  exit 1
fi

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -Fxq "${node_container}"; then
  echo "compose node container is not running: ${node_container}" >&2
  exit 1
fi

push_output="$(IMAGE="${source_image}" bash "${AXERN_ROOT}/scripts/dev-env/registry-image-push.sh")"
cluster_image="$(printf '%s\n' "${push_output}" | awk -F= '$1 == "cluster_image" {print $2}')"
if [ -z "${cluster_image}" ]; then
  printf '%s\n' "${push_output}" >&2
  echo "registry push did not return cluster_image" >&2
  exit 1
fi

namespace="compose-registry-image-smoke-$(date +%s)"
run_id=""
environment_id=""
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"

dump_registry_image_smoke_failure() {
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
  echo "--- ${node_container}: imagemgr inventory ---" >&2
  docker exec "${node_container}" curl -fsS --unix-socket /run/imagemgr/imagemgr.sock http://unix/inventory 2>/dev/null | python3 -m json.tool >&2 || true
}

cleanup_registry_image_smoke() {
  local rc=$?
  if [ "${rc}" -ne 0 ]; then
    dump_registry_image_smoke_failure || true
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
  end_env_lock compose
  return "${rc}"
}
trap cleanup_registry_image_smoke EXIT

create_output="$(
  local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" run --detach -o json \
      --namespace "${namespace}" \
      "${cluster_image}" -- python -c 'print("compose-registry-image-smoke-ok")' || true
)"
if ! run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${create_output}" 2>/dev/null)"; then
  printf '%s\n' "${create_output}" >&2
  echo "failed to parse created run id" >&2
  exit 1
fi
if ! run_json="$(local_smoke_wait_for_run_terminal "${run_id}")"; then
  printf '%s\n' "${run_json}" >&2
  echo "run ${run_id} did not reach a terminal status" >&2
  exit 1
fi
run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${run_json}")"
environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"].get("environment_id",""))' <<<"${run_json}")"
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

echo "compose_registry_image_smoke_ok=true"
echo "source_image=${source_image}"
echo "cluster_image=${cluster_image}"
