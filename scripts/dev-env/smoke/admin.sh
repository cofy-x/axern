local_smoke_assert_consistency_ok() {
  local env_name="$1"
  local endpoint="$2"
  local consistency_json
  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  consistency_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin consistency check -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data.get("status") == "ok", data' <<<"${consistency_json}" >/dev/null
}

local_smoke_report_reliability() {
  local env_name="$1"
  local endpoint="$2"
  local attempt stdout_file stderr_file stderr_body reliability_json node_json
  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  node_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin node list --status active -o json)"
  python3 -c 'import json,sys; nodes=json.load(sys.stdin).get("nodes") or []; assert nodes, "no active nodes"; assert all(node.get("lifecycle_status") == "active" for node in nodes), nodes' <<<"${node_json}" >/dev/null
  stdout_file="$(mktemp)"
  stderr_file="$(mktemp)"
  for attempt in 1 2 3; do
    if "${AXERN_SMOKE_CMD[@]}" admin reliability check -o json >"${stdout_file}" 2>"${stderr_file}"; then
      break
    fi
    stderr_body="$(cat "${stderr_file}")"
    if [ "${attempt}" -lt 3 ] && local_smoke_is_transient_grpc_error "${stderr_body}"; then
      sleep 2
      continue
    fi
    break
  done
  reliability_json="$(cat "${stdout_file}")"
  stderr_body="$(cat "${stderr_file}")"
  rm -f "${stdout_file}" "${stderr_file}"
  if [ -n "${reliability_json}" ] && python3 -m json.tool >/dev/null 2>&1 <<<"${reliability_json}"; then
    python3 -c '
import json
import sys

data = json.load(sys.stdin)
signals = data.get("signals") or []
node_fleet = data.get("node_fleet_health") or {}
if int(node_fleet.get("active_nodes") or 0) < 1:
    raise SystemExit(f"admin reliability has no active nodes: {node_fleet!r}")
consistency = data.get("consistency") or {}
counts = consistency.get("counts") or {}
consistency_issues = counts.get("issues") or 0
print(
    "admin_reliability_status={status} signals={signals} consistency_issues={consistency_issues}".format(
        status=data.get("status", "unknown"),
        signals=len(signals),
        consistency_issues=consistency_issues,
    )
)
if consistency_issues:
    raise SystemExit(1)
' <<<"${reliability_json}" >&2
    return $?
  fi
  if [ -n "${reliability_json}" ]; then
    printf '%s\n' "${reliability_json}" >&2
  fi
  if [ -n "${stderr_body}" ]; then
    printf '%s\n' "${stderr_body}" >&2
  fi
  echo "admin reliability check did not return readable health JSON; continuing as non-blocking report" >&2
  return 0
}

local_smoke_compose_psql() {
  local compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
  local compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}")
  if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
    compose_args+=(--profile otel)
  fi
  docker compose "${compose_args[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U postgres -d axern "$@"
}

local_smoke_k8s_psql() {
  kubectl -n "${K8S_NAMESPACE}" exec -i deployment/postgres -- psql -v ON_ERROR_STOP=1 -U postgres -d axern "$@"
}

local_smoke_psql() {
  local env_name="$1"
  shift
  case "${env_name}" in
    compose)
      local_smoke_compose_psql "$@"
      ;;
    kind|k8s)
      local_smoke_k8s_psql "$@"
      ;;
    *)
      echo "psql smoke helper does not support env ${env_name}" >&2
      return 1
      ;;
  esac
}

local_smoke_assert_service_volume_storage_state() {
  local env_name="$1"
  local namespace="$2"
  local claim_status="$3"
  local binding_status="$4"
  local rows
  rows="$(local_smoke_psql "${env_name}" -At \
    -v "namespace=${namespace}" \
    -v "claim_status=${claim_status}" \
    -v "binding_status=${binding_status}" <<'SQL'
WITH target_claims AS (
  SELECT claim_id, status
  FROM storage_volume_claims
  WHERE namespace = :'namespace'
), target_bindings AS (
  SELECT binding_id, status
  FROM storage_volume_bindings
  WHERE namespace = :'namespace'
)
SELECT
  (SELECT count(*) FROM target_claims WHERE status = :'claim_status') || '|' ||
  (SELECT count(*) FROM target_bindings WHERE status = :'binding_status') || '|' ||
  (SELECT count(*) FROM target_bindings WHERE status <> 'VOLUME_STATUS_DELETED') || '|' ||
  (SELECT count(*) FROM target_claims);
SQL
)"
  python3 -c '
import sys

raw = sys.argv[1].strip()
claim_status = sys.argv[2]
binding_status = sys.argv[3]
try:
    claim_count, binding_count, active_bindings, total_claims = [int(x) for x in raw.split("|")]
except Exception as exc:
    raise SystemExit(f"unreadable storage status row {raw!r}: {exc}") from exc
if total_claims < 1:
    raise SystemExit("expected at least one volume claim")
if claim_count < 1:
    raise SystemExit(f"expected at least one claim in {claim_status}, got {claim_count}")
if binding_count < 1:
    raise SystemExit(f"expected at least one binding in {binding_status}, got {binding_count}")
if binding_status == "VOLUME_STATUS_DELETED" and active_bindings != 0:
    raise SystemExit(f"expected no active bindings after release, got {active_bindings}")
' "${rows}" "${claim_status}" "${binding_status}"
}

local_smoke_assert_compose_admin_repair_actions() {
  local endpoint="$1"
  local namespace="admin-repair-smoke"
  local suffix node_id
  local force_allocation_id force_run_id fail_allocation_id fail_run_id clear_allocation_id clear_run_id
  suffix="$(date +%s)-$$"
  force_allocation_id="alloc-admin-repair-smoke-force-${suffix}"
  force_run_id="run-admin-repair-smoke-force-${suffix}"
  fail_allocation_id="alloc-admin-repair-smoke-fail-${suffix}"
  fail_run_id="run-admin-repair-smoke-fail-${suffix}"
  clear_allocation_id="alloc-admin-repair-smoke-clear-${suffix}"
  clear_run_id="run-admin-repair-smoke-clear-${suffix}"

  local_smoke_init_axern_cmd compose "${endpoint}"
  node_id="$(local_smoke_compose_psql -Atc "SELECT node_id FROM nodes ORDER BY node_id LIMIT 1")"
  if [ -z "${node_id}" ]; then
    echo "admin repair smoke requires at least one registered compose node" >&2
    return 1
  fi

  cleanup_local_smoke_admin_repair_actions() {
    local rc=$?
    local_smoke_compose_psql \
      -v "force_allocation_id=${force_allocation_id}" \
      -v "fail_allocation_id=${fail_allocation_id}" \
      -v "clear_allocation_id=${clear_allocation_id}" \
      -v "force_run_id=${force_run_id}" \
      -v "fail_run_id=${fail_run_id}" \
      -v "clear_run_id=${clear_run_id}" <<'SQL' >/dev/null 2>&1 || true
DELETE FROM allocation_reconcile_queue
WHERE allocation_id IN (:'force_allocation_id', :'fail_allocation_id', :'clear_allocation_id');
DELETE FROM admin_audit_events
WHERE target_id IN (:'force_allocation_id', :'fail_allocation_id', :'clear_allocation_id');
DELETE FROM runs
WHERE run_id IN (:'force_run_id', :'fail_run_id', :'clear_run_id')
   OR allocation_id IN (:'force_allocation_id', :'fail_allocation_id', :'clear_allocation_id');
DELETE FROM allocations
WHERE allocation_id IN (:'force_allocation_id', :'fail_allocation_id', :'clear_allocation_id');
SQL
    return "${rc}"
  }
  trap cleanup_local_smoke_admin_repair_actions RETURN

  seed_local_smoke_admin_repair_retry() {
    local allocation_id="$1"
    local run_id="$2"
    local due="$3"
    local_smoke_compose_psql \
      -v "allocation_id=${allocation_id}" \
      -v "run_id=${run_id}" \
      -v "node_id=${node_id}" \
      -v "namespace=${namespace}" \
      -v "due=${due}" <<'SQL' >/dev/null
INSERT INTO allocations (
  allocation_id, owner_type, owner_id, environment_id, node_id, attempt,
  status, config, created_at, updated_at
) VALUES (
  :'allocation_id', 'run', :'run_id', 'env-admin-repair-smoke', :'node_id', 1,
  'ALLOCATION_STATUS_RESERVED', '{}'::jsonb, now(), now()
);
INSERT INTO runs (
  run_id, namespace, environment_id, allocation_id, attempt,
  status, config, labels, created_at, updated_at
) VALUES (
  :'run_id', :'namespace', 'env-admin-repair-smoke', :'allocation_id', 1,
  'RUN_STATUS_PLACED', '{}'::jsonb, '{}'::jsonb, now(), now()
);
INSERT INTO allocation_reconcile_queue (
  allocation_id, reason, next_run_at, reconcile_attempts, last_error, created_at, updated_at
) VALUES (
  :'allocation_id', 'create',
  CASE WHEN :'due' = 'true' THEN now() ELSE now() + interval '1 hour' END,
  1, 'admin repair smoke seed', now(), now()
);
SQL
  }

  local list_json force_json fail_json clear_json audit_json
  seed_local_smoke_admin_repair_retry "${force_allocation_id}" "${force_run_id}" false
  list_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin allocation-retry list --reason create -o json)"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert any(r.get("allocation_id") == sys.argv[1] for r in data.get("retries", [])), data' "${force_allocation_id}" <<<"${list_json}" >/dev/null

  force_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin allocation-retry force "${force_allocation_id}" --reason create --operator-reason "compose smoke force retry" -o json)"
  python3 -c 'import json,sys; retry=json.load(sys.stdin)["retry"]; assert retry["allocation_id"] == sys.argv[1] and retry["due"] is True, retry' "${force_allocation_id}" <<<"${force_json}" >/dev/null

  seed_local_smoke_admin_repair_retry "${fail_allocation_id}" "${fail_run_id}" false
  fail_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin allocation-retry fail "${fail_allocation_id}" --operator-reason "compose smoke fail retry" -o json)"
  python3 -c 'import json,sys; retry=json.load(sys.stdin)["retry"]; assert retry["allocation_id"] == sys.argv[1] and retry["reason"] == "create", retry' "${fail_allocation_id}" <<<"${fail_json}" >/dev/null

  seed_local_smoke_admin_repair_retry "${clear_allocation_id}" "${clear_run_id}" false
  local_smoke_compose_psql \
    -v "allocation_id=${clear_allocation_id}" \
    -v "run_id=${clear_run_id}" <<'SQL' >/dev/null
UPDATE allocations
SET status = 'ALLOCATION_STATUS_FAILED', updated_at = now()
WHERE allocation_id = :'allocation_id';
UPDATE runs
SET status = 'RUN_STATUS_FAILED', updated_at = now()
WHERE run_id = :'run_id';
SQL
  clear_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin allocation-retry clear "${clear_allocation_id}" --reason create --operator-reason "compose smoke clear retry" -o json)"
  python3 -c 'import json,sys; retry=json.load(sys.stdin)["retry"]; assert retry["allocation_id"] == sys.argv[1] and retry["clearable"] is True, retry' "${clear_allocation_id}" <<<"${clear_json}" >/dev/null

  audit_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin audit list --target-type allocation -o json)"
  python3 -c '
import json
import sys

events = json.load(sys.stdin).get("events", [])
seen = {(event.get("target_id"), event.get("operation")) for event in events}
want = {
    (sys.argv[1], "force-allocation-lifecycle-retry"),
    (sys.argv[2], "fail-allocation-lifecycle-retry"),
    (sys.argv[3], "clear-allocation-lifecycle-retry"),
}
assert want.issubset(seen), seen
' "${force_allocation_id}" "${fail_allocation_id}" "${clear_allocation_id}" <<<"${audit_json}" >/dev/null

  echo "compose_admin_repair_smoke_ok=true"
}
