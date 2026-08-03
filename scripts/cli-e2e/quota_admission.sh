#!/usr/bin/env bash

verify_quota_admission() {
  local namespace quota_env_output quota_environment_id quota_events_output quota_events_count quota_get_output quota_list_output quota_list_contains quota_unset_output quota_run_output quota_run_id quota_service_output quota_service_id quota_service_json quota_service_status quota_service_message quota_service_diagnostic quota_service_admission deadline
  local quota_cpu_limit quota_memory_limit quota_reserved_cpu quota_reserved_memory quota_unset_cpu quota_unset_memory
  namespace="e2e-quota-admission"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace create "${namespace}" -o json >"${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota set --namespace "${namespace}" --cpu 100m --memory 1GiB -o json >"${cli_object_output}"
  quota_get_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota get --namespace "${namespace}" -o json)"
  quota_cpu_limit="$(json_query "quota admission quota get" 'json.load(sys.stdin)["quota"]["cpu_milli_limit"]' "${quota_get_output}")"
  quota_memory_limit="$(json_query "quota admission quota get" 'json.load(sys.stdin)["quota"]["memory_bytes_limit"]' "${quota_get_output}")"
  quota_reserved_cpu="$(json_query "quota admission quota get" 'json.load(sys.stdin)["quota"]["reserved_cpu_milli"]' "${quota_get_output}")"
  quota_reserved_memory="$(json_query "quota admission quota get" 'json.load(sys.stdin)["quota"]["reserved_memory_bytes"]' "${quota_get_output}")"
  if [ "${quota_cpu_limit}" != "100" ] || [ "${quota_memory_limit}" != "1073741824" ] || [ "${quota_reserved_cpu}" != "0" ] || [ "${quota_reserved_memory}" != "0" ]; then
    echo "quota admission quota get returned unexpected usage or limits" >&2
    printf '%s\n' "${quota_get_output}" >&2
    dump_logs
    exit 1
  fi
  quota_list_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota list --constrained --sort pressure -o json)"
  quota_list_contains="$(json_query "quota admission quota list" "any(item.get('namespace') == '${namespace}' for item in json.load(sys.stdin).get('quotas', []))" "${quota_list_output}")"
  if [ "${quota_list_contains}" != "True" ]; then
    echo "quota admission quota list did not include namespace ${namespace}" >&2
    cat "${cli_error_output}" >&2 || true
    printf '%s\n' "${quota_list_output}" >&2
    dump_logs
    exit 1
  fi
  quota_list_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota list --sort pressure --limit 0 -o json)"
  quota_list_contains="$(json_query "quota admission sorted quota list" "any(item.get('namespace') == '${namespace}' for item in json.load(sys.stdin).get('quotas', []))" "${quota_list_output}")"
	if [ "${quota_list_contains}" != "True" ]; then
    echo "quota admission sorted quota list did not include namespace ${namespace}" >&2
    printf '%s\n' "${quota_list_output}" >&2
    dump_logs
    exit 1
  fi

  quota_env_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --namespace "${namespace}" --template-id python311)"
  quota_environment_id="$(json_query "quota admission environment create" 'json.load(sys.stdin)["environment"]["id"]' "${quota_env_output}")"
  [ -n "${quota_environment_id}" ] || {
    echo "quota admission environment create did not return an environment id" >&2
    dump_logs
    exit 1
  }

  if "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run \
    -o json \
    --namespace "${namespace}" \
    --environment "${quota_environment_id}" \
    -- /bin/sh -lc 'sleep 1' >"${cli_object_output}" 2>"${cli_error_output}"; then
    echo "quota-limited run unexpectedly succeeded" >&2
    cat "${cli_object_output}" >&2 || true
    dump_logs
    exit 1
  fi

  if ! grep -Eiq "ResourceExhausted|resource exhausted|namespace quota|quota" "${cli_error_output}"; then
    echo "quota-limited run returned an unexpected error" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi

  quota_service_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create \
    -o json \
    --namespace "${namespace}" \
    --environment-id "${quota_environment_id}" \
    --replicas 1 \
    --argv /bin/sh \
    --argv -lc \
    --argv 'sleep 30' 2>"${cli_error_output}")"
  quota_service_id="$(json_query "quota-limited service create" 'json.load(sys.stdin)["service"]["id"]' "${quota_service_output}")"
  [ -n "${quota_service_id}" ] || {
    echo "quota-limited service create did not return a service id" >&2
    dump_logs
    exit 1
  }

  quota_service_json=""
  deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    quota_service_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${quota_service_id}" -o json 2>"${cli_error_output}" || true)"
    quota_service_status="$(json_query "quota-limited service get" 'json.load(sys.stdin)["service"].get("status", "")' "${quota_service_json}")"
    quota_service_message="$(json_query "quota-limited service get" 'json.load(sys.stdin)["service"].get("message", "")' "${quota_service_json}")"
    if [ "${quota_service_status}" = "degraded" ] && grep -Eiq "namespace quota|quota" <<<"${quota_service_message}"; then
      break
    fi
    sleep 1
  done
  if [ "${quota_service_status}" != "degraded" ] || ! grep -Eiq "namespace quota|quota" <<<"${quota_service_message}"; then
    echo "quota-limited service did not report degraded quota admission failure in time" >&2
    printf '%s\n' "${quota_service_json}" >&2
    dump_logs
    exit 1
  fi
  quota_service_diagnostic="$(json_query "quota-limited service get" 'json.load(sys.stdin)["service"].get("diagnostic_code", "")' "${quota_service_json}")"
  quota_service_admission="$(json_query "quota-limited service get" 'json.load(sys.stdin)["service"].get("admission_summary", "")' "${quota_service_json}")"
  if [ "${quota_service_diagnostic}" != "admission-blocked" ] || [ "${quota_service_admission}" != "namespace quota exceeded" ]; then
    echo "quota-limited service did not expose stable admission diagnostic JSON fields" >&2
    printf '%s\n' "${quota_service_json}" >&2
    dump_logs
    exit 1
  fi
  quota_events_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota events --namespace "${namespace}" --limit 10 -o json)"
  quota_events_count="$(json_query "quota admission quota events" 'len(json.load(sys.stdin).get("events", []))' "${quota_events_output}")"
  if [ "${quota_events_count}" -lt 2 ]; then
    echo "quota admission events did not include run and service rejections" >&2
    printf '%s\n' "${quota_events_output}" >&2
    dump_logs
    exit 1
  fi
  quota_list_contains="$(json_query "quota admission quota events" 'all(event.get("type") == "admission-rejected" and event.get("reason") for event in json.load(sys.stdin).get("events", []))' "${quota_events_output}")"
  if [ "${quota_list_contains}" != "True" ]; then
    echo "quota admission events did not expose stable event type and reason fields" >&2
    printf '%s\n' "${quota_events_output}" >&2
    dump_logs
    exit 1
  fi
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service delete "${quota_service_id}" -o json >"${cli_object_output}"

  quota_unset_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota unset --namespace "${namespace}" -o json)"
  quota_unset_cpu="$(json_query "quota admission quota unset" 'json.load(sys.stdin)["quota"].get("cpu_milli_limit")' "${quota_unset_output}")"
  quota_unset_memory="$(json_query "quota admission quota unset" 'json.load(sys.stdin)["quota"].get("memory_bytes_limit")' "${quota_unset_output}")"
  if [ "${quota_unset_cpu}" != "None" ] || [ "${quota_unset_memory}" != "None" ]; then
    echo "quota admission quota unset did not return unlimited quota" >&2
    printf '%s\n' "${quota_unset_output}" >&2
    dump_logs
    exit 1
  fi
  quota_get_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota get --namespace "${namespace}" -o json)"
  quota_unset_cpu="$(json_query "quota admission quota get after unset" 'json.load(sys.stdin)["quota"].get("cpu_milli_limit")' "${quota_get_output}")"
  quota_unset_memory="$(json_query "quota admission quota get after unset" 'json.load(sys.stdin)["quota"].get("memory_bytes_limit")' "${quota_get_output}")"
  if [ "${quota_unset_cpu}" != "None" ] || [ "${quota_unset_memory}" != "None" ]; then
    echo "quota admission quota get after unset did not return unlimited quota" >&2
    printf '%s\n' "${quota_get_output}" >&2
    dump_logs
    exit 1
  fi

  if "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run \
    -o json \
    --namespace "${namespace}" \
    --environment "${quota_environment_id}" \
    --request-memory 1048576GiB \
    -- /bin/sh -lc 'sleep 1' >"${cli_object_output}" 2>"${cli_error_output}"; then
    echo "oversized-memory run unexpectedly succeeded" >&2
    cat "${cli_object_output}" >&2 || true
    dump_logs
    exit 1
  fi

  if ! grep -Eiq "no eligible node|insufficient_memory|resource exhausted" "${cli_error_output}" || ! grep -Eiq "insufficient_memory" "${cli_error_output}"; then
    echo "oversized-memory run returned an unexpected placement error" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi

  if "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run \
    -o json \
    --namespace "${namespace}" \
    --environment "${quota_environment_id}" \
    --runtime-class unsupported-e2e-runtime \
    -- /bin/sh -lc 'sleep 1' >"${cli_object_output}" 2>"${cli_error_output}"; then
    echo "unsupported-runtime run unexpectedly succeeded" >&2
    cat "${cli_object_output}" >&2 || true
    dump_logs
    exit 1
  fi

  if ! grep -Eiq "no eligible node|runtime_unsupported|unsupported" "${cli_error_output}" || grep -Eiq "insufficient_cpu|insufficient_memory|effective_allocatable|namespace quota" "${cli_error_output}"; then
    echo "unsupported-runtime run returned an unexpected node-selection error" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi

  quota_run_output=""
  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if quota_run_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run --detach \
      -o json \
      --namespace "${namespace}" \
      --environment "${quota_environment_id}" \
      -- /bin/sh -lc 'sleep 30' 2>"${cli_error_output}")"; then
      break
    fi
    if grep -q "no eligible node" "${cli_error_output}"; then
      sleep 1
      continue
    fi
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  done

  if [ -z "${quota_run_output}" ]; then
    echo "quota admission recovery run did not find an eligible node in time" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  quota_run_id="$(json_query "quota admission recovery run" 'json.load(sys.stdin)["run"]["id"]' "${quota_run_output}")"
  [ -n "${quota_run_id}" ] || {
    echo "quota admission recovery run did not return a run id" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run cancel "${quota_run_id}" -o json >"${cli_object_output}"
}
