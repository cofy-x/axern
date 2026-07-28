#!/usr/bin/env bash

verify_secret_environment_service_rollout() {
  secret_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" secret create -o json --type opaque --literal token=hello-secret)"
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

  service_env_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --template-id python311)"
  service_environment_id="$(json_query "environment create rollout base" 'json.load(sys.stdin)["environment"]["id"]' "${service_env_create_output}")"
  [ -n "${service_environment_id}" ] || {
    echo "axern environment create rollout base did not return an environment id" >&2
    dump_logs
    exit 1
  }
  service_env_next_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --template-id python311)"
  next_service_environment_id="$(json_query "environment create rollout replacement" 'json.load(sys.stdin)["environment"]["id"]' "${service_env_next_output}")"
  [ -n "${next_service_environment_id}" ] || {
    echo "axern environment create rollout replacement did not return an environment id" >&2
    dump_logs
    exit 1
  }

  service_create_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create \
    -o json \
    --environment-id "${service_environment_id}" \
    --replicas 1 \
    --request-cpu 250m \
    --request-memory 128MiB \
    --argv /bin/sh \
    --argv -lc \
    --secret-env "TOKEN=${secret_id}:token" \
    --argv 'sleep 300')"
  service_id="$(json_query "service create" 'json.load(sys.stdin)["service"]["id"]' "${service_create_output}")"
  [ -n "${service_id}" ] || {
    echo "axern service create did not return a service id" >&2
    dump_logs
    exit 1
  }
  service_create_request_cpu="$(json_query "service create" 'json.load(sys.stdin)["service"]["config"]["resources"]["requests"]["cpu_milli"]' "${service_create_output}")"
  service_create_request_memory="$(json_query "service create" 'json.load(sys.stdin)["service"]["config"]["resources"]["requests"]["memory_bytes"]' "${service_create_output}")"
  if [ "${service_create_request_cpu}" != "250" ] || [ "${service_create_request_memory}" != "134217728" ]; then
    echo "service create did not persist requested resources" >&2
    printf '%s\n' "${service_create_output}" >&2
    dump_logs
    exit 1
  fi
  wait_for_ready_service_allocation "${service_id}" "service before rollout" >/dev/null

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${service_id}" >"${cli_object_output}"
  if ! grep -q "${service_id}" "${cli_object_output}"; then
    echo "axern service get text output did not include ${service_id}" >&2
    dump_logs
    exit 1
  fi
  service_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${service_id}" -o json)"
  service_get_environment_id="$(json_query "service get" 'json.load(sys.stdin)["service"]["environment_id"]' "${service_get_json}")"
  [ "${service_get_environment_id}" = "${service_environment_id}" ] || {
    echo "axern service get returned environment ${service_get_environment_id}, want ${service_environment_id}" >&2
    dump_logs
    exit 1
  }
  grep -q "\"rollout_policy\"" <<<"${service_get_json}"

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service list >"${cli_object_output}"
  if ! grep -q "${service_id}" "${cli_object_output}"; then
    echo "axern service list text output did not include ${service_id}" >&2
    dump_logs
    exit 1
  fi

  service_update_output=""
  deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${service_id}" -o json)"
    service_version="$(json_query "service get" 'json.load(sys.stdin)["service"]["version"]' "${service_get_json}")"
    if service_update_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service update "${service_id}" \
      -o json \
      --expected-version "${service_version}" \
      --environment-id "${next_service_environment_id}" \
      --request-cpu 500m \
      --request-memory 256MiB \
      --max-surge 1 \
      --max-unavailable 0 2>"${cli_error_output}")"; then
      break
    fi
    if grep -q "service version mismatch" "${cli_error_output}"; then
      sleep 1
      continue
    fi
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  done
  [ -n "${service_update_output}" ] || {
    echo "axern service update did not succeed in time" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  }
  grep -q "\"service\"" <<<"${service_update_output}"
  service_update_request_cpu="$(json_query "service update" 'json.load(sys.stdin)["service"]["config"]["resources"]["requests"]["cpu_milli"]' "${service_update_output}")"
  service_update_request_memory="$(json_query "service update" 'json.load(sys.stdin)["service"]["config"]["resources"]["requests"]["memory_bytes"]' "${service_update_output}")"
  if [ "${service_update_request_cpu}" != "500" ] || [ "${service_update_request_memory}" != "268435456" ]; then
    echo "service update did not persist requested resources" >&2
    printf '%s\n' "${service_update_output}" >&2
    dump_logs
    exit 1
  fi
  service_update_argv="$(json_query "service update" 'json.load(sys.stdin)["service"]["config"]["argv"]' "${service_update_output}")"
  service_update_max_surge="$(json_query "service update" 'json.load(sys.stdin)["service"]["rollout_policy"]["max_surge"]' "${service_update_output}")"
  if [ "${service_update_argv}" != "['/bin/sh', '-lc', 'sleep 300']" ] || [ "${service_update_max_surge}" != "1" ]; then
    echo "service update did not preserve argv or rollout surge policy" >&2
    printf '%s\n' "${service_update_output}" >&2
    dump_logs
    exit 1
  fi
  service_update_rollout="$(json_query "service update" 'json.load(sys.stdin)["service"].get("rollout_status") is not None' "${service_update_output}")"
  [ "${service_update_rollout}" = "True" ] || {
    echo "axern service update did not report rollout status" >&2
    dump_logs
    exit 1
  }
  updated_allocation_id="$(wait_for_ready_service_allocation "${service_id}" "updated service")"
  updated_replicas_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service replicas "${service_id}" --view updated -o json)"
  updated_allocation_seen="$(json_query "service replicas --view updated" "any(replica.get('id') == '${updated_allocation_id}' and not replica.get('outdated', False) for replica in json.load(sys.stdin).get('replicas', []))" "${updated_replicas_json}")"
  [ "${updated_allocation_seen}" = "True" ] || {
    echo "service replicas --view updated did not include ready replacement ${updated_allocation_id}" >&2
    dump_logs
    exit 1
  }
}
