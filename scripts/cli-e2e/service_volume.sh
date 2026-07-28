#!/usr/bin/env bash

verify_service_volume() {
  volume_env_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --template-id python311)"
  volume_environment_id="$(json_query "environment create volume env" 'json.load(sys.stdin)["environment"]["id"]' "${volume_env_output}")"
  [ -n "${volume_environment_id}" ] || {
    echo "axern environment create for volume scenario did not return an environment id" >&2
    dump_logs
    exit 1
  }
  volume_service_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create \
    -o json \
    --environment-id "${volume_environment_id}" \
    --runtime-class runc \
    --replicas 1 \
    --volume data:/var/lib/app:rw,rbind \
    --argv /bin/sh \
    --argv -lc \
    --argv 'sleep 120')"
  volume_service_id="$(json_query "service create volume scenario" 'json.load(sys.stdin)["service"]["id"]' "${volume_service_output}")"
  [ -n "${volume_service_id}" ] || {
    echo "axern service create for volume scenario did not return a service id" >&2
    dump_logs
    exit 1
  }
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${volume_service_id}" >"${cli_object_output}"
  grep -q "${volume_service_id}" "${cli_object_output}"
  volume_service_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${volume_service_id}" -o json)"
  grep -q "\"volume_mounts\"" <<<"${volume_service_get_json}"
  volume_service_update_output=""
  deadline=$((SECONDS + 30))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    volume_service_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${volume_service_id}" -o json)"
    volume_service_version="$(json_query "service get volume scenario" 'json.load(sys.stdin)["service"]["version"]' "${volume_service_get_json}")"
    if volume_service_update_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service update "${volume_service_id}" \
      -o json \
      --expected-version "${volume_service_version}" \
      --volume data:/var/lib/app:ro,rbind 2>"${cli_error_output}")"; then
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
  [ -n "${volume_service_update_output}" ] || {
    echo "axern service update for volume scenario did not succeed in time" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  }
  grep -q "\"service\"" <<<"${volume_service_update_output}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${volume_service_id}" >"${cli_object_output}"
  grep -q "${volume_service_id}" "${cli_object_output}"
  volume_service_get_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service get "${volume_service_id}" -o json)"
  grep -q "\"readonly\": true" <<<"${volume_service_get_json}"
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service delete "${volume_service_id}" -o json >"${cli_object_output}"
  grep -q "\"id\": \"${volume_service_id}\"" "${cli_object_output}"
}
