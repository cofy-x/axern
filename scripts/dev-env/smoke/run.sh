run_local_run_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local namespace="${prefix}-${env_name}-run-smoke-$(date +%s)"
  local catalog_json env_json quota_json quota_list run_json run_get run_list cancel_json default_run_json failed_run_json
  local run_id="" default_run_id="" failed_run_id="" environment_id=""
  local rejected_run_error="" quota_error=""
  local quota_set="false"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  cleanup_local_run_smoke() {
    local rc=$?
    local id
    for id in "${run_id:-}" "${default_run_id:-}" "${failed_run_id:-}"; do
      if [ -n "${id}" ]; then
        local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${id}" -o json >/dev/null 2>&1 || true
        local_smoke_wait_for_run_terminal "${id}" >/dev/null 2>&1 || true
      fi
    done
    if [ -n "${environment_id:-}" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null 2>&1 || true
      environment_id=""
    fi
    if [ "${quota_set}" = "true" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota unset -o json --namespace "${namespace}" >/dev/null 2>&1 || true
      quota_set="false"
    fi
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null 2>&1 || true
    if [ -n "${quota_error:-}" ]; then
      rm -f "${quota_error}"
      quota_error=""
    fi
    if [ -n "${rejected_run_error:-}" ]; then
      rm -f "${rejected_run_error}"
      rejected_run_error=""
    fi
    return "${rc}"
  }
  trap cleanup_local_run_smoke RETURN

  catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog list -o json)"
  local_smoke_assert_default_runtime_templates "${catalog_json}"

  env_json="$(local_smoke_create_environment "${namespace}")"
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

  quota_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota set -o json --namespace "${namespace}" --cpu 1 --memory 8GiB)"
  quota_set="true"
  python3 -c 'import json,sys; data=json.load(sys.stdin)["quota"]; assert data["namespace"] == sys.argv[1] and data["cpu_milli_limit"] == 1000 and data["memory_bytes_limit"] == 8589934592' "${namespace}" <<<"${quota_json}" >/dev/null
  quota_list="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota list --constrained --sort pressure -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert any(item["namespace"] == sys.argv[1] for item in data["quotas"])' "${namespace}" <<<"${quota_list}" >/dev/null
  quota_list="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota list --sort pressure --limit 0 -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert any(item["namespace"] == sys.argv[1] for item in data["quotas"])' "${namespace}" <<<"${quota_list}" >/dev/null

  run_json="$(local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" "${AXERN_SMOKE_CMD[@]}" run create -o json --namespace "${namespace}" --environment-id "${environment_id}" --argv python --argv -c --argv 'import time; time.sleep(60)')"
  run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${run_json}")"
  [ -n "${run_id}" ]
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["status"] != "failed"' <<<"${run_json}" >/dev/null

  run_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run get -o json "${run_id}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["id"] == sys.argv[1]' "${run_id}" <<<"${run_get}" >/dev/null

  run_list="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run list -o json --namespace "${namespace}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert any(item["id"] == sys.argv[1] for item in data["runs"])' "${run_id}" <<<"${run_list}" >/dev/null

  quota_error="$(mktemp)"
  if "${AXERN_SMOKE_CMD[@]}" run create -o json --namespace "${namespace}" --environment-id "${environment_id}" --request-cpu 600m --request-memory 128MiB >/dev/null 2>"${quota_error}"; then
    echo "run create over namespace quota succeeded unexpectedly" >&2
    return 1
  fi
  if ! grep -q "namespace quota exceeded" "${quota_error}"; then
    cat "${quota_error}" >&2 || true
    return 1
  fi
  rm -f "${quota_error}"
  quota_error=""

  cancel_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${run_id}" -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["id"] == sys.argv[1]' "${run_id}" <<<"${cancel_json}" >/dev/null
  run_get="$(local_smoke_wait_for_run_status "${run_id}" cancelled)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["status"] == "cancelled"' <<<"${run_get}" >/dev/null
  run_id=""
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota unset -o json --namespace "${namespace}" >/dev/null
  quota_set="false"

  default_run_json="$(local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" "${AXERN_SMOKE_CMD[@]}" run create -o json --namespace "${namespace}" --environment-id "${environment_id}")"
  default_run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${default_run_json}")"
  [ -n "${default_run_id}" ]
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["status"] != "failed"' <<<"${default_run_json}" >/dev/null
	local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${default_run_id}" -o json >/dev/null 2>&1 || true
	local_smoke_wait_for_run_terminal "${default_run_id}" >/dev/null 2>&1 || true
	default_run_id=""

  rejected_run_error="$(mktemp)"
  if "${AXERN_SMOKE_CMD[@]}" run create -o json --namespace "${namespace}" --environment-id "${environment_id}" --argv " " --argv python >/dev/null 2>"${rejected_run_error}"; then
    echo "run create with blank argv succeeded unexpectedly" >&2
    return 1
  fi
  if ! grep -q "config.argv\\[0\\] must be non-empty" "${rejected_run_error}"; then
    cat "${rejected_run_error}" >&2 || true
    return 1
  fi
  rm -f "${rejected_run_error}"
  rejected_run_error=""

  failed_run_json="$(local_smoke_json_once_or_recover_by_namespace run runs run "${namespace}" "${AXERN_SMOKE_CMD[@]}" run create -o json --namespace "${namespace}" --environment-id "${environment_id}" --argv python --argv -c --argv 'import sys; sys.exit(42)')"
  failed_run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${failed_run_json}")"
  [ -n "${failed_run_id}" ]
  run_get="$(local_smoke_wait_for_run_status "${failed_run_id}" failed)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["run"]["status"] == "failed" and data["run"].get("exit_code_known", False) and data["run"].get("exit_code") == 42' <<<"${run_get}" >/dev/null
  failed_run_id=""
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null
  environment_id=""
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null

  echo "${prefix}_run_smoke_ok=true"
}
