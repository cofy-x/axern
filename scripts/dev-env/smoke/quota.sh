run_local_quota_admission_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local namespace="${prefix}-${env_name}-quota-smoke-$(date +%s)"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  local catalog_json env_json run_error quota_list quota_unset_json
  local quota_set="false" environment_id=""
  run_error=""
  cleanup_local_quota_admission_smoke() {
    local rc=$?
    if [ -n "${environment_id:-}" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null 2>&1 || true
      environment_id=""
    fi
    if [ "${quota_set}" = "true" ]; then
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota unset --namespace "${namespace}" -o json >/dev/null 2>&1 || true
      quota_set="false"
    fi
    local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null 2>&1 || true
    if [ -n "${run_error:-}" ]; then
      rm -f "${run_error}"
    fi
    return "${rc}"
  }
  trap cleanup_local_quota_admission_smoke RETURN

  catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog list -o json)"
  local_smoke_assert_default_runtime_templates "${catalog_json}"

  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace create "${namespace}" -o json >/dev/null
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota set --namespace "${namespace}" --cpu 100m --memory 1GiB -o json >/dev/null
  quota_set="true"
  quota_list="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota list --constrained --sort pressure -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert any(item["namespace"] == sys.argv[1] for item in data["quotas"])' "${namespace}" <<<"${quota_list}" >/dev/null
  quota_list="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota list --pressure -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert all(max((item.get("reserved_cpu_milli") or 0) * 100 // item["cpu_milli_limit"] if item.get("cpu_milli_limit") else 0, (item.get("reserved_memory_bytes") or 0) * 100 // item["memory_bytes_limit"] if item.get("memory_bytes_limit") else 0) >= 80 for item in data.get("quotas", []))' <<<"${quota_list}" >/dev/null
  env_json="$(local_smoke_create_environment "${namespace}")"
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

  run_error="$(mktemp)"
  if "${AXERN_SMOKE_CMD[@]}" run -o json --namespace "${namespace}" --environment "${environment_id}" >/dev/null 2>"${run_error}"; then
    echo "quota admission run succeeded unexpectedly" >&2
    return 1
  fi
  if ! grep -q "namespace quota exceeded" "${run_error}"; then
    cat "${run_error}" >&2 || true
    return 1
  fi
  rm -f "${run_error}"
  run_error=""

  quota_unset_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" quota unset --namespace "${namespace}" -o json)"
  python3 -c '
import json, sys
quota = json.load(sys.stdin)["quota"]
assert quota.get("cpu_milli_limit") is None
assert quota.get("memory_bytes_limit") is None
' <<<"${quota_unset_json}" >/dev/null
  quota_set="false"
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment delete "${environment_id}" -o json >/dev/null
  environment_id=""
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" -o json >/dev/null
  echo "${prefix}_quota_admission_smoke_ok=true"
}
