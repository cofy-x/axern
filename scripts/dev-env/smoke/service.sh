run_local_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local service_prefix="$3"
  local cli_env
  cli_env="$(cli_env_file "${env_name}")"
  # shellcheck disable=SC1090
  source "${cli_env}"
  local axern_bin
  axern_bin="$(local_smoke_axern_bin)"
  local axern_timeout="${LOCAL_SMOKE_AXERN_TIMEOUT:-20s}"
  local -a axern_cmd=("${axern_bin}" "--endpoint" "${endpoint}" "--timeout" "${axern_timeout}")
  AXERN_SMOKE_CMD=("${axern_cmd[@]}")
  local namespace="${service_prefix}-${env_name}-smoke-$(date +%s)"
  local catalog_json bundle_catalog_json env_json secret_json quota_json quota_get service_json service_get service_replicas service_events
  local service_id=""
  local quota_set="false"
  cleanup_local_smoke_service() {
    local rc=$?
    if [ -n "${service_id:-}" ]; then
      local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
      service_id=""
    fi
    if [ "${quota_set:-false}" = "true" ]; then
      local_smoke_retry_json "${axern_cmd[@]}" quota unset -o json --namespace "${namespace}" >/dev/null 2>&1 || true
      quota_set="false"
    fi
    return "${rc}"
  }
  trap cleanup_local_smoke_service RETURN

  catalog_json="$(local_smoke_retry_json "${axern_cmd[@]}" catalog list -o json)"
  local_smoke_assert_default_runtime_templates "${catalog_json}"
  bundle_catalog_json="$(local_smoke_retry_json "${axern_cmd[@]}" catalog bundle list -o json)"
  local_smoke_assert_default_agent_bundles "${bundle_catalog_json}"
  env_json="$(local_smoke_create_environment "${namespace}")"
  local environment_id
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"
  secret_json="$(local_smoke_create_secret "${namespace}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["secret"]["id"]' <<<"${secret_json}" >/dev/null
  quota_json="$(local_smoke_retry_json "${axern_cmd[@]}" quota set -o json --namespace "${namespace}" --cpu 2 --memory 8GiB)"
  quota_set="true"
  python3 -c 'import json,sys; q=json.load(sys.stdin)["quota"]; assert q["namespace"] == sys.argv[1] and q["cpu_milli_limit"] == 2000 and q["memory_bytes_limit"] == 8589934592' "${namespace}" <<<"${quota_json}" >/dev/null
  local service_script
  service_script='import http.server, socketserver; socketserver.TCPServer.allow_reuse_address=True; http.server.ThreadingHTTPServer(("0.0.0.0", 8080), http.server.SimpleHTTPRequestHandler).serve_forever()'
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" "${axern_cmd[@]}" service create -o json --namespace "${namespace}" --environment-id "${environment_id}" --replicas 1 --argv python --argv -u --argv -c --argv "${service_script}")"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"
  local deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${axern_cmd[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -z "${service_get}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["service"]["status"] == "ready"' <<<"${service_get}" >/dev/null
  service_replicas="$(local_smoke_retry_json "${axern_cmd[@]}" service replicas -o json "${service_id}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); replicas=data["replicas"]; assert len(replicas) >= 1; assert not [replica for replica in replicas if replica.get("lifecycle_retry")]' <<<"${service_replicas}" >/dev/null
  quota_get="$(local_smoke_retry_json "${axern_cmd[@]}" quota get -o json --namespace "${namespace}")"
  python3 -c 'import json,sys; q=json.load(sys.stdin)["quota"]; assert q["reserved_cpu_milli"] > 0 and q["reserved_memory_bytes"] > 0' <<<"${quota_get}" >/dev/null
  service_events="$(local_smoke_retry_json "${axern_cmd[@]}" service events -o json "${service_id}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert "events" in data' <<<"${service_events}" >/dev/null
  local_smoke_delete_service "${service_id}"
  service_id=""
  quota_get="$(local_smoke_retry_json "${axern_cmd[@]}" quota get -o json --namespace "${namespace}")"
  python3 -c 'import json,sys; q=json.load(sys.stdin)["quota"]; assert q["reserved_cpu_milli"] == 0 and q["reserved_memory_bytes"] == 0' <<<"${quota_get}" >/dev/null
  local_smoke_retry_json "${axern_cmd[@]}" quota unset -o json --namespace "${namespace}" >/dev/null
  quota_set="false"
  echo "${service_prefix}_smoke_ok=true"
}
