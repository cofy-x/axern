run_local_quota_admission_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local namespace="${prefix}-${env_name}-quota-smoke-$(date +%s)"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  local catalog_json env_json run_error service_json service_get service_events service_id quota_list quota_unset_json
  local quota_set="false" environment_id=""
  run_error=""
  service_id=""
  cleanup_local_quota_admission_smoke() {
    local rc=$?
    if [ -n "${service_id:-}" ]; then
      local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
    fi
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

  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" --environment-id "${environment_id}" \
    --replicas 1 --argv /bin/sh --argv -lc --argv 'sleep 30')"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"
  [ -n "${service_id}" ]

  local deadline=$((SECONDS + 40))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c '
import json, sys
service = json.load(sys.stdin)["service"]
message = service.get("message") or ""
sys.exit(0 if service.get("status") == "degraded" and "namespace quota exceeded" in message else 1)
' <<<"${service_get}"; then
      break
    fi
    sleep 1
  done
  python3 -c '
import json, sys
service = json.load(sys.stdin)["service"]
message = service.get("message") or ""
assert service.get("status") == "degraded" and "namespace quota exceeded" in message
assert service.get("diagnostic_code") == "admission-blocked"
assert service.get("admission_summary") == "namespace quota exceeded"
' <<<"${service_get}" >/dev/null

  service_events="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${service_id}")"
  python3 -c '
import json, sys
events = json.load(sys.stdin).get("events", [])
assert any(event.get("diagnostic_code") == "admission-blocked" for event in events)
' <<<"${service_events}" >/dev/null

  local_smoke_delete_service "${service_id}"
  service_id=""
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
