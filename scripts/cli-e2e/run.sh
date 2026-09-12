#!/usr/bin/env bash

verify_run() {
  local secret_create_output=""
  local secret_id=""
  local secret_get_id=""
  local secret_list_seen=""
  local registry_secret_output=""
  local registry_secret_id=""
  local registry_secret_get_id=""
  local run_create_output=""
  local run_status=""
  local run_id=""

  secret_create_output="$(printf '%s\n' 'token=hello-secret' | "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret create -o json --type opaque --literal-stdin)"
  secret_id="$(json_query "secret create" 'json.load(sys.stdin)["secret"]["id"]' "${secret_create_output}")"
  [ -n "${secret_id}" ] || {
    echo "axern secret create did not return a secret id" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret get "${secret_id}" -o json >"${cli_object_output}"
  secret_get_id="$(json_query "secret get" 'json.load(sys.stdin)["secret"]["id"]' "$(cat "${cli_object_output}")")"
  [ "${secret_get_id}" = "${secret_id}" ] || {
    echo "axern secret get returned ${secret_get_id}, want ${secret_id}" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret list -o json >"${cli_object_output}"
  secret_list_seen="$(json_query "secret list" "any(secret.get('id') == '${secret_id}' for secret in json.load(sys.stdin).get('secrets', []))" "$(cat "${cli_object_output}")")"
  [ "${secret_list_seen}" = "True" ] || {
    echo "axern secret list did not include ${secret_id}" >&2
    dump_logs
    exit 1
  }

  printf '%s' '{"auths":{}}' >"${docker_secret_file}"
  registry_secret_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret create -o json --type docker-config-json --file "${docker_secret_file}")"
  registry_secret_id="$(json_query "secret create --type docker-config-json" 'json.load(sys.stdin)["secret"]["id"]' "${registry_secret_output}")"
  [ -n "${registry_secret_id}" ] || {
    echo "axern secret create --type docker-config-json did not return a secret id" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret get "${registry_secret_id}" -o json >"${cli_object_output}"
  registry_secret_get_id="$(json_query "secret get docker-config-json" 'json.load(sys.stdin)["secret"]["id"]' "$(cat "${cli_object_output}")")"
  [ "${registry_secret_get_id}" = "${registry_secret_id}" ] || {
    echo "axern secret get docker-config-json returned ${registry_secret_get_id}, want ${registry_secret_id}" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret delete "${registry_secret_id}" -o json >"${cli_object_output}"
  grep -q "\"id\": \"${registry_secret_id}\"" "${cli_object_output}"

  local deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if run_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run --detach \
      -o json \
      --environment "${environment_id}" \
      --secret-env "TOKEN=${secret_id}:token" \
      -- /bin/sh -lc 'sleep 60' 2>"${cli_error_output}")"; then
      run_status="$(json_query "run" 'json.load(sys.stdin)["run"].get("status", "")' "${run_create_output}")"
      if [ "${run_status}" != "6" ] && ! grep -qi "FAILED" <<<"${run_status}"; then
        break
      fi
      json_query "run" 'json.load(sys.stdin)["run"].get("message", "")' "${run_create_output}" >"${cli_error_output}" || true
      run_create_output=""
      sleep 1
      continue
    fi
    if grep -q "no eligible node" "${cli_error_output}"; then
      sleep 1
      continue
    fi
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  done
  if [ -z "${run_create_output}" ]; then
    echo "axern run did not find an eligible node in time" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  run_id="$(json_query "run" 'json.load(sys.stdin)["run"]["id"]' "${run_create_output}")"
  [ -n "${run_id}" ] || {
    echo "axern run did not return a run id" >&2
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run get "${run_id}" -o json >"${cli_object_output}"
  grep -q "\"id\": \"${run_id}\"" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run list -o json >"${cli_object_output}"
  grep -q "${run_id}" "${cli_object_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run cancel "${run_id}" -o json >"${cli_object_output}"
  grep -q "${run_id}" "${cli_object_output}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret delete "${secret_id}" -o json >"${cli_object_output}"
  grep -q "\"id\": \"${secret_id}\"" "${cli_object_output}"
}
