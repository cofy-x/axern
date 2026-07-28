#!/usr/bin/env bash

create_base_environment() {
  env_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --template-id python311)"
  environment_id="$(json_query "environment create" 'json.load(sys.stdin)["environment"]["id"]' "${env_create_output}")"
  [ -n "${environment_id}" ] || {
    echo "axern environment create did not return an environment id" >&2
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment get "${environment_id}" -o json >"${cli_object_output}"
  grep -q "\"id\": \"${environment_id}\"" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment list -o json >"${cli_object_output}"
  grep -q "${environment_id}" "${cli_object_output}"
}
