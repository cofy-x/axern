#!/usr/bin/env bash

verify_catalog_namespace_quota() {
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog list >"${cli_catalog_output}"
  grep -q "python311" "${cli_catalog_output}"
  grep -q "server-base" "${cli_catalog_output}"
  grep -q "coding-base" "${cli_catalog_output}"
  grep -q "desktop-base" "${cli_catalog_output}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog get python311 >"${cli_template_output}"
  grep -q "^ID: python311$" "${cli_template_output}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog get server-base >"${cli_template_output}"
  grep -q "^ID: server-base$" "${cli_template_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog get coding-base >"${cli_template_output}"
  grep -q "^ID: coding-base$" "${cli_template_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog get desktop-base >"${cli_template_output}"
  grep -q "^ID: desktop-base$" "${cli_template_output}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog bundle list >"${cli_catalog_output}"
  grep -q "claude-code" "${cli_catalog_output}"
  grep -q "codex" "${cli_catalog_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog bundle get claude-code >"${cli_template_output}"
  grep -q "^ID: claude-code$" "${cli_template_output}"
  grep -q "^Binary Path: /bin/claude$" "${cli_template_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" catalog bundle get codex >"${cli_template_output}"
  grep -q "^ID: codex$" "${cli_template_output}"
  grep -q "^Binary Path: /bin/codex$" "${cli_template_output}"

  namespace_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace create e2e-team -o json)"
  namespace_name="$(json_query "namespace create" 'json.load(sys.stdin)["namespace"]["namespace"]' "${namespace_output}")"
  [ "${namespace_name}" = "e2e-team" ] || {
    echo "axern namespace create returned namespace ${namespace_name}, want e2e-team" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace get e2e-team -o json >"${cli_object_output}"
  grep -q "\"namespace\": \"e2e-team\"" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace list -o json >"${cli_object_output}"
  grep -q "\"namespace\": \"e2e-team\"" "${cli_object_output}"

  environment_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create --namespace e2e-team --template-id python311 -o json)"
  environment_id="$(json_query "environment create" 'json.load(sys.stdin)["environment"]["id"]' "${environment_output}")"
  [ -n "${environment_id}" ] || {
    echo "axern environment create did not return an environment id" >&2
    dump_logs
    exit 1
  }
  if "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace delete e2e-team -o json >"${cli_object_output}" 2>"${cli_error_output}"; then
    echo "axern namespace delete succeeded while a live environment existed" >&2
    dump_logs
    exit 1
  fi
  if ! grep -q "live environments" "${cli_error_output}"; then
    echo "axern namespace delete returned unexpected live-environment blocker" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  environment_delete_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment delete "${environment_id}" -o json)"
  environment_status="$(json_query "environment delete" 'json.load(sys.stdin)["environment"]["status"]' "${environment_delete_output}")"
  [ "${environment_status}" = "deleted" ] || {
    echo "axern environment delete returned status ${environment_status}, want deleted" >&2
    dump_logs
    exit 1
  }

  quota_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota set --namespace e2e-team --cpu 2 --memory 1GiB -o json)"
  quota_namespace="$(json_query "quota set" 'json.load(sys.stdin)["quota"]["namespace"]' "${quota_output}")"
  quota_cpu_limit="$(json_query "quota set" 'json.load(sys.stdin)["quota"]["cpu_milli_limit"]' "${quota_output}")"
  quota_memory_limit="$(json_query "quota set" 'json.load(sys.stdin)["quota"]["memory_bytes_limit"]' "${quota_output}")"
  [ "${quota_namespace}" = "e2e-team" ] || {
    echo "axern quota set returned namespace ${quota_namespace}, want e2e-team" >&2
    dump_logs
    exit 1
  }
  [ "${quota_cpu_limit}" = "2000" ] || {
    echo "axern quota set returned cpu limit ${quota_cpu_limit}, want 2000" >&2
    dump_logs
    exit 1
  }
  [ "${quota_memory_limit}" = "1073741824" ] || {
    echo "axern quota set returned memory limit ${quota_memory_limit}, want 1073741824" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota get --namespace e2e-team -o json >"${cli_object_output}"
  grep -q "\"namespace\": \"e2e-team\"" "${cli_object_output}"
  grep -q "\"cpu_milli_limit\": 2000" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota get --namespace e2e-team -o json >"${cli_object_output}"
  grep -q "\"quota\"" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota list -o json >"${cli_object_output}"
  grep -q "\"namespace\": \"e2e-team\"" "${cli_object_output}"
  quota_unset_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" quota unset --namespace e2e-team -o json)"
  quota_unset_cpu="$(json_query "quota unset" 'json.load(sys.stdin)["quota"].get("cpu_milli_limit")' "${quota_unset_output}")"
  [ "${quota_unset_cpu}" = "None" ] || {
    echo "axern quota unset returned cpu limit ${quota_unset_cpu}, want None" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace delete e2e-team -o json >"${cli_object_output}"
  if "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" namespace get e2e-team -o json >"${cli_object_output}" 2>"${cli_error_output}"; then
    echo "axern namespace get succeeded after delete" >&2
    dump_logs
    exit 1
  fi
}
