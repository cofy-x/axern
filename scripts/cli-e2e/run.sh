#!/usr/bin/env bash

verify_run() {
  local run_create_output=""
  local run_status=""
  local run_id=""
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
