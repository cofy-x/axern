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

run_local_service_volume_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local service_prefix="$3"
  local namespace="${service_prefix}-${env_name}-service-volume-smoke-$(date +%s)"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  local catalog_json env_json service_json service_get service_update_output
  local cli_error_output=""
  local service_id=""
  cleanup_local_service_volume_smoke() {
    local rc=$?
    if [ -n "${service_id:-}" ]; then
      local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
      service_id=""
    fi
    if [ -n "${cli_error_output:-}" ]; then
      rm -f "${cli_error_output}"
      cli_error_output=""
    fi
    return "${rc}"
  }
  trap cleanup_local_service_volume_smoke RETURN

  echo "${service_prefix}_service_volume_smoke_phase=create" >&2
  catalog_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog list -o json)"
  local_smoke_assert_default_runtime_templates "${catalog_json}"

  env_json="$(local_smoke_create_environment "${namespace}")"
  local environment_id
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

  local service_script_initial service_script_updated service_script_reattached
  service_script_initial="$(cat <<'SH'
set -eu
if [ ! -f /tmp/sentinel.txt ]; then
  printf '%s\n' 'local-service-volume-sentinel' >/tmp/sentinel.txt
fi
exec python -m http.server 8080 --directory /tmp
SH
)"
  service_script_updated="$(cat <<'SH'
set -eu
expected='local-service-volume-sentinel'
test -f /tmp/sentinel.txt
test "$(tr -d '\n' </tmp/sentinel.txt)" = "${expected}"
printf '%s\n' 'replacement-observed' >/tmp/updated.txt
exec python -m http.server 8080 --directory /tmp
SH
)"
  service_script_reattached="$(cat <<'SH'
set -eu
expected='local-service-volume-sentinel'
test -f /tmp/sentinel.txt
test "$(tr -d '\n' </tmp/sentinel.txt)" = "${expected}"
printf '%s\n' 'reattached-observed' >/tmp/reattached.txt
exec python -m http.server 8080 --directory /tmp
SH
)"

  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --volume data:/tmp:rw,rbind \
    --argv /bin/sh --argv -lc --argv "${service_script_initial}" \
    --readiness-http-port 8080 --readiness-http-path /sentinel.txt --readiness-period 1s --readiness-timeout 1s)"
  local service_version
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"

  local deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -z "${service_get}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1' <<<"${service_get}" >/dev/null
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_PUBLISHED" "VOLUME_STATUS_PUBLISHED"
  if [ "${AXERN_SERVICE_VOLUME_RESTART_SMOKE:-true}" = "true" ]; then
    echo "${service_prefix}_service_volume_smoke_phase=restart_initial" >&2
    local_smoke_restart_node_runtime "${env_name}"
    local_smoke_wait_service_ready "${service_id}"
    local_smoke_assert_service_volume_http_body "${env_name}" "${namespace}" "${service_id}" 8080 /sentinel.txt local-service-volume-sentinel
    local_smoke_assert_node_volume_reliability_ok
    local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_PUBLISHED" "VOLUME_STATUS_PUBLISHED"
  fi

  echo "${service_prefix}_service_volume_smoke_phase=rollout_update" >&2
  cli_error_output="$(mktemp)"
  service_update_output=""
  deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}")"
    service_version="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["version"])' <<<"${service_get}")"
    if service_update_output="$("${AXERN_SMOKE_CMD[@]}" service update "${service_id}" -o json \
      --expected-version "${service_version}" \
      --volume data:/tmp:rw,rbind \
      --argv /bin/sh --argv -lc --argv "${service_script_updated}" \
      --max-surge 1 --max-unavailable 0 2>"${cli_error_output}")"; then
      break
    fi
    if grep -q "service version mismatch" "${cli_error_output}"; then
      sleep 1
      continue
    fi
    cat "${cli_error_output}" >&2 || true
    return 1
  done
  [ -n "${service_update_output}" ] || {
    cat "${cli_error_output}" >&2 || true
    return 1
  }
  rm -f "${cli_error_output}"
  cli_error_output=""

  deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -z "${service_get}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; data=json.load(sys.stdin); config=data["service"]["config"]; mounts=config.get("volume_mounts", []); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 and mounts and mounts[0]["target"] == "/tmp" else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1' <<<"${service_get}" >/dev/null
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_PUBLISHED" "VOLUME_STATUS_PUBLISHED"
  local_smoke_assert_service_volume_http_body "${env_name}" "${namespace}" "${service_id}" 8080 /updated.txt replacement-observed
  if [ "${AXERN_SERVICE_VOLUME_RESTART_SMOKE:-true}" = "true" ]; then
    echo "${service_prefix}_service_volume_smoke_phase=restart_updated" >&2
    local_smoke_restart_node_runtime "${env_name}"
    local_smoke_wait_service_ready "${service_id}"
    local_smoke_assert_service_volume_http_body "${env_name}" "${namespace}" "${service_id}" 8080 /updated.txt replacement-observed
    local_smoke_assert_node_volume_reliability_ok
    local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_PUBLISHED" "VOLUME_STATUS_PUBLISHED"
  fi

  local_smoke_delete_service "${service_id}"
  service_id=""
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_BOUND" "VOLUME_STATUS_DELETED"

  echo "${service_prefix}_service_volume_smoke_phase=reattach_retained_claim" >&2
  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --volume data:/tmp:rw,rbind \
    --argv /bin/sh --argv -lc --argv "${service_script_reattached}" \
    --readiness-http-port 8080 --readiness-http-path /reattached.txt --readiness-period 1s --readiness-timeout 1s)"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"
  local_smoke_wait_service_ready "${service_id}"
  local_smoke_assert_service_volume_http_body "${env_name}" "${namespace}" "${service_id}" 8080 /reattached.txt reattached-observed
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_PUBLISHED" "VOLUME_STATUS_PUBLISHED"
  local_smoke_delete_service "${service_id}"
  service_id=""
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_BOUND" "VOLUME_STATUS_DELETED"

  echo "${service_prefix}_service_volume_smoke_phase=failure_injection" >&2
  local_smoke_service_volume_failure_smoke "${env_name}" "${service_prefix}"

  echo "${service_prefix}_service_volume_smoke_ok=true"
}

local_smoke_wait_service_ready() {
  local service_id="$1"
  local service_get deadline
  deadline=$((SECONDS + 180))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -z "${service_get}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] >= 1 else 1)' <<<"${service_get}"; then
      return 0
    fi
    sleep 2
  done
  echo "service ${service_id} did not become ready after node runtime restart" >&2
  return 1
}

local_smoke_restart_node_runtime() {
  local env_name="$1"
  case "${env_name}" in
    compose)
      docker restart "${COMPOSE_PROJECT_NAME}-node-1" >/dev/null
      ;;
    k8s|kind)
      kubectl -n "${K8S_NAMESPACE}" rollout restart daemonset/node-all-in-one >/dev/null
      kubectl -n "${K8S_NAMESPACE}" rollout status daemonset/node-all-in-one --timeout=240s >/dev/null
      ;;
    *)
      echo "service volume restart smoke does not know how to restart node runtime for ${env_name}" >&2
      return 1
      ;;
  esac
}

local_smoke_assert_node_volume_reliability_ok() {
  local reliability_json deadline
  reliability_json=""
  deadline=$((SECONDS + 180))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    reliability_json="$("${AXERN_SMOKE_CMD[@]}" admin reliability check -o json 2>/dev/null || true)"
    if [ -n "${reliability_json}" ] && python3 -c '
import json
import sys

data = json.load(sys.stdin)
signals = data.get("signals") or []
node_volume_signal_codes = {
    "node-volume-managers",
    "node_volume_managers",
    "ADMIN_RELIABILITY_SIGNAL_CODE_NODE_VOLUME_MANAGERS",
}
if any(signal.get("code") in node_volume_signal_codes for signal in signals):
    raise SystemExit(f"unexpected node volume manager reliability signal after restart: {signals!r}")
node_volume_health = data.get("node_volume_health") or {}
unhealthy_nodes = int(node_volume_health.get("unhealthy_nodes") or 0)
if unhealthy_nodes != 0:
    raise SystemExit(f"unexpected unhealthy node volume managers after restart: {node_volume_health!r}")
node_fleet = data.get("node_fleet_health") or {}
active_nodes = int(node_fleet.get("active_nodes") or 0)
ready_nodes = int(node_fleet.get("ready_nodes") or 0)
if node_fleet.get("unavailable") or active_nodes == 0 or ready_nodes != active_nodes:
    raise SystemExit(f"node fleet has not converged after restart: {node_fleet!r}")
if any(int(node_fleet.get(key) or 0) != 0 for key in ("stale_heartbeat_nodes", "stale_summary_nodes", "not_ready_nodes")):
    raise SystemExit(f"node fleet remains unhealthy after restart: {node_fleet!r}")
' <<<"${reliability_json}" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "node and volume reliability did not converge after node runtime restart" >&2
  [ -z "${reliability_json}" ] || printf '%s\n' "${reliability_json}" >&2
  return 1
}

local_smoke_service_volume_failure_smoke() {
  local env_name="$1"
  local service_prefix="$2"
  local namespace="${service_prefix}-${env_name}-service-volume-failure-smoke-$(date +%s)"
  local env_json service_json service_get
  local service_id=""
  local blocked_claim_id=""
  local node_runtime_paused="false"
  local previous_return_trap
  previous_return_trap="$(trap -p RETURN || true)"
  cleanup_local_service_volume_failure_smoke() {
    local rc=$?
    if [ -n "${blocked_claim_id:-}" ]; then
      local_smoke_unblock_service_volume_claim "${env_name}" "${blocked_claim_id}"
      blocked_claim_id=""
    fi
    if [ "${node_runtime_paused:-false}" = "true" ]; then
      docker unpause "${COMPOSE_PROJECT_NAME}-node-1" >/dev/null 2>&1 || true
      node_runtime_paused="false"
    fi
    if [ -n "${service_id:-}" ]; then
      local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
      service_id=""
    fi
    trap - RETURN
    if [ -n "${previous_return_trap:-}" ]; then
      eval "${previous_return_trap}"
    fi
    return "${rc}"
  }
  trap cleanup_local_service_volume_failure_smoke RETURN

  env_json="$(local_smoke_create_environment "${namespace}")"
  local environment_id
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"
  if [ "${env_name}" = "compose" ]; then
    docker pause "${COMPOSE_PROJECT_NAME}-node-1" >/dev/null
    node_runtime_paused="true"
  fi

  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --volume data:/tmp:rw,rbind \
    --argv /bin/sh --argv -lc --argv "sleep 300")"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"

  local claim_id=""
  local claim_deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${claim_deadline}" ]; do
    claim_id="$(local_smoke_psql "${env_name}" -At -v "namespace=${namespace}" <<'SQL'
SELECT claim_id
FROM storage_volume_claims
WHERE namespace = :'namespace' AND status <> 'VOLUME_STATUS_DELETED'
ORDER BY created_at DESC
LIMIT 1;
SQL
)"
    claim_id="$(printf '%s' "${claim_id}" | tr -d '[:space:]')"
    [ -n "${claim_id}" ] && break
    sleep 1
  done
  [ -n "${claim_id}" ] || {
    echo "service volume failure smoke did not observe a claim for ${namespace}" >&2
    return 1
  }
  local_smoke_block_service_volume_claim "${env_name}" "${claim_id}"
  blocked_claim_id="${claim_id}"
  if [ "${node_runtime_paused}" = "true" ]; then
    docker unpause "${COMPOSE_PROJECT_NAME}-node-1" >/dev/null
    node_runtime_paused="false"
  fi

  local failed_state_observed="false"
  local deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_FAILED" "VOLUME_STATUS_FAILED" >/dev/null 2>&1; then
      failed_state_observed="true"
      break
    fi
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ]; then
      python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] in {"degraded","failed"} else 1)' <<<"${service_get}" >/dev/null 2>&1 || true
    fi
    sleep 2
  done
  if [ "${failed_state_observed}" != "true" ]; then
    if ! local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_FAILED" "VOLUME_STATUS_FAILED"; then
      return 1
    fi
  fi
  if ! local_smoke_assert_storage_reliability_signal; then
    return 1
  fi
  if ! local_smoke_assert_service_volume_failure_surface "${namespace}" "${service_id}"; then
    return 1
  fi

  local_smoke_delete_service "${service_id}"
  service_id=""
  local_smoke_unblock_service_volume_claim "${env_name}" "${claim_id}"
  blocked_claim_id=""
  local_smoke_assert_service_volume_storage_state "${env_name}" "${namespace}" "VOLUME_STATUS_BOUND" "VOLUME_STATUS_DELETED"
}

local_smoke_assert_service_volume_failure_surface() {
  local namespace="$1"
  local service_id="$2"
  local service_get diagnostic storage_bindings deadline
  deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ]; then
      diagnostic="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("service", {}).get("diagnostic_code", ""))' <<<"${service_get}")"
      if [ "${diagnostic}" = "volume-publish-error" ]; then
        return 0
      fi
    fi
    storage_bindings="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin storage list --status failed --namespace "${namespace}" -o json 2>/dev/null || true)"
    if [ -n "${storage_bindings}" ] && python3 -c '
import json
import sys

service_id = sys.argv[1]
payload = json.load(sys.stdin)
for binding in payload.get("bindings", []):
    if (
        binding.get("status") == "failed"
        and binding.get("workload_id") == service_id
        and (binding.get("message") or "").strip()
    ):
        raise SystemExit(0)
raise SystemExit(1)
' "${service_id}" <<<"${storage_bindings}"; then
      return 0
    fi
    sleep 2
  done
  echo "service volume failure surface did not expose volume-publish-error or failed admin storage binding with a message for service ${service_id}" >&2
  if [ -n "${service_get:-}" ]; then
    echo "last service get:" >&2
    printf '%s\n' "${service_get}" >&2
  fi
  if [ -n "${storage_bindings:-}" ]; then
    echo "last failed storage bindings:" >&2
    printf '%s\n' "${storage_bindings}" >&2
  fi
  return 1
}

local_smoke_block_service_volume_claim() {
  local env_name="$1"
  local claim_id="$2"
  local remote_path="/var/lib/volumed/local/${claim_id}"
  case "${env_name}" in
    compose)
      mkdir -p "${COMPOSE_STATE_DIR}/volumed/local"
      rm -rf "${COMPOSE_STATE_DIR}/volumed/local/${claim_id}"
      printf blocked > "${COMPOSE_STATE_DIR}/volumed/local/${claim_id}"
      ;;
    k8s|kind)
      local pods pod
      pods="$(kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
      [ -n "${pods}" ] || {
        echo "missing node-all-in-one pods for service volume failure injection" >&2
        return 1
      }
      while IFS= read -r pod; do
        [ -n "${pod}" ] || continue
        kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc "set -eu; mkdir -p /var/lib/volumed/local; rm -rf '${remote_path}'; printf blocked > '${remote_path}'"
      done <<<"${pods}"
      ;;
    *)
      echo "service volume failure smoke does not know how to inject node-local storage failure for ${env_name}" >&2
      return 1
      ;;
  esac
}

local_smoke_unblock_service_volume_claim() {
  local env_name="$1"
  local claim_id="$2"
  local remote_path="/var/lib/volumed/local/${claim_id}"
  case "${env_name}" in
    compose)
      rm -f "${COMPOSE_STATE_DIR}/volumed/local/${claim_id}" >/dev/null 2>&1 || true
      ;;
    k8s|kind)
      local pods pod
      pods="$(kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)"
      while IFS= read -r pod; do
        [ -n "${pod}" ] || continue
        kubectl -n "${K8S_NAMESPACE}" exec "${pod}" -- sh -lc "rm -f '${remote_path}'" >/dev/null 2>&1 || true
      done <<<"${pods}"
      ;;
  esac
}

local_smoke_assert_storage_reliability_signal() {
  local reliability_json
  reliability_json="$("${AXERN_SMOKE_CMD[@]}" admin reliability check -o json)"
  python3 -c '
import json,sys
data=json.load(sys.stdin)
signals=data.get("health", data).get("signals", [])
if not any(signal.get("code") in {"storage_bindings", "storage-bindings", "ADMIN_RELIABILITY_SIGNAL_CODE_STORAGE_BINDINGS", "node_volume_managers", "node-volume-managers", "ADMIN_RELIABILITY_SIGNAL_CODE_NODE_VOLUME_MANAGERS"} for signal in signals):
    raise SystemExit(f"expected storage reliability signal, got {signals!r}")
' <<<"${reliability_json}"
}

local_smoke_service_gateway_target() {
  local env_name="$1"
  case "${env_name}" in
    compose)
      printf '127.0.0.1:%s\n' "${COMPOSE_GATEWAY_HTTP_PORT}"
      ;;
    kind|k8s)
      printf '127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_HTTP_PORT}"
      ;;
    *)
      echo "service gateway helper does not support env ${env_name}" >&2
      return 1
      ;;
  esac
}

local_smoke_assert_service_volume_http_body() {
  local env_name="$1"
  local namespace="$2"
  local service_id="$3"
  local port="$4"
  local path="$5"
  local expected="$6"
  local gateway_target body deadline
  gateway_target="$(local_smoke_service_gateway_target "${env_name}")"
  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -le "${deadline}" ]; do
    body="$(curl --connect-timeout 2 --max-time 10 -fsS "http://${gateway_target}/svc/${namespace}/${service_id}/${port}${path}" 2>/dev/null || true)"
    if [ "${body}" = "${expected}" ]; then
      return 0
    fi
    sleep 2
  done
  echo "service volume HTTP body ${body}, want ${expected}" >&2
  return 1
}
