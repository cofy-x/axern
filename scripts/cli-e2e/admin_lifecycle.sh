#!/usr/bin/env bash

verify_admin_lifecycle() {
  assert_reconcile_health_debug_endpoint

  docker stop -t 1 "${NODE_CONTAINER_NAME}" >/dev/null

  admin_run_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run --detach \
    -o json \
    --environment "${environment_id}" \
    -- /bin/sh -lc 'sleep 60' 2>"${cli_error_output}")" || {
    echo "admin lifecycle run did not return an accepted run" >&2
    dump_logs
    exit 1
  }
  admin_run_id="$(json_query "admin lifecycle run" 'json.load(sys.stdin)["run"]["id"]' "${admin_run_output}")"
  admin_allocation_id="$(json_query "admin lifecycle run" 'json.load(sys.stdin)["run"]["allocation_id"]' "${admin_run_output}")"
  [ -n "${admin_run_id}" ] && [ -n "${admin_allocation_id}" ] || {
    echo "admin lifecycle run did not return run and allocation ids" >&2
    dump_logs
    exit 1
  }

  deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin allocation-retry list \
      --reason create \
      -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
      dump_logs
      exit 1
    }
    if grep -q "${admin_allocation_id}" "${cli_object_output}"; then
      break
    fi
    sleep 1
  done
  if ! grep -q "${admin_allocation_id}" "${cli_object_output}"; then
    echo "admin lifecycle retry did not appear in list output" >&2
    dump_logs
    exit 1
  fi
  assert_allocation_reconcilez_contains "${admin_allocation_id}" "create"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin allocation-retry force "${admin_allocation_id}" \
    --reason create \
    --operator-reason "cli e2e force allocation lifecycle retry" \
    -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  grep -q "\"allocation_id\": \"${admin_allocation_id}\"" "${cli_object_output}" || {
    dump_logs
    exit 1
  }
  grep -q '"due": true' "${cli_object_output}" || {
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin audit list \
    --operation force-allocation-lifecycle-retry \
    --target-type allocation \
    --target-id "${admin_allocation_id}" \
    -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  grep -q '"operation": "force-allocation-lifecycle-retry"' "${cli_object_output}" || {
    dump_logs
    exit 1
  }
  grep -q "\"target_id\": \"${admin_allocation_id}\"" "${cli_object_output}" || {
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin allocation-retry fail "${admin_allocation_id}" \
    --operator-reason "cli e2e fail allocation lifecycle retry" \
    -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  grep -q "\"allocation_id\": \"${admin_allocation_id}\"" "${cli_object_output}" || {
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin audit list \
    --operation fail-allocation-lifecycle-retry \
    --target-type allocation \
    --target-id "${admin_allocation_id}" \
    -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  grep -q '"operation": "fail-allocation-lifecycle-retry"' "${cli_object_output}" || {
    dump_logs
    exit 1
  }
  grep -q "cli e2e fail allocation lifecycle retry" "${cli_object_output}" || {
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" admin allocation-retry list \
    --reason create \
    -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  if grep -q "${admin_allocation_id}" "${cli_object_output}"; then
    echo "admin lifecycle retry still listed after fail" >&2
    dump_logs
    exit 1
  fi
  assert_allocation_reconcilez_absent "${admin_allocation_id}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run get "${admin_run_id}" -o json >"${cli_object_output}" 2>"${cli_error_output}" || {
    dump_logs
    exit 1
  }
  grep -q '"status": "failed"' "${cli_object_output}" || {
    dump_logs
    exit 1
  }
  grep -q "cli e2e fail allocation lifecycle retry" "${cli_object_output}" || {
    dump_logs
    exit 1
  }
}

assert_reconcile_health_debug_endpoint() {
  local body
  body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/reconcilez" 2>"${cli_error_output}")" || {
    echo "reconcilez debug endpoint is not reachable" >&2
    dump_logs
    exit 1
  }
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
components = {item.get("component") for item in payload.get("components", [])}
missing = {"run", "node", "service", "tunnel"} - components
if missing:
    raise SystemExit(f"reconcilez missing components: {sorted(missing)}")
' <<<"${body}" 2>"${cli_error_output}" || {
    echo "reconcilez debug endpoint did not expose all expected components" >&2
    dump_logs
    exit 1
  }
}

assert_allocation_reconcilez_contains() {
  local allocation_id="$1"
  local reason="$2"
  local body
  body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/allocation-reconcilez" 2>"${cli_error_output}")" || {
    echo "allocation-reconcilez debug endpoint is not reachable" >&2
    dump_logs
    exit 1
  }
  python3 -c '
import json
import sys

allocation_id, reason = sys.argv[1], sys.argv[2]
payload = json.load(sys.stdin)
for item in payload.get("items", []):
    if item.get("allocation_id") == allocation_id and item.get("reason") == reason:
        raise SystemExit(0)
raise SystemExit(f"allocation-reconcilez missing {allocation_id} reason={reason}")
' "${allocation_id}" "${reason}" <<<"${body}" 2>"${cli_error_output}" || {
    echo "allocation-reconcilez did not expose the queued retry" >&2
    dump_logs
    exit 1
  }
}

assert_allocation_reconcilez_absent() {
  local allocation_id="$1"
  local body
  body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/allocation-reconcilez" 2>"${cli_error_output}")" || {
    echo "allocation-reconcilez debug endpoint is not reachable" >&2
    dump_logs
    exit 1
  }
  python3 -c '
import json
import sys

allocation_id = sys.argv[1]
payload = json.load(sys.stdin)
for item in payload.get("items", []):
    if item.get("allocation_id") == allocation_id:
        raise SystemExit(f"allocation-reconcilez still includes {allocation_id}")
' "${allocation_id}" <<<"${body}" 2>"${cli_error_output}" || {
    echo "allocation-reconcilez still exposed a completed retry" >&2
    dump_logs
    exit 1
  }
}
