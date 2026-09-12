#!/usr/bin/env bash

verify_ssh_gateway() {
  local run_output run_id allocation_id ssh_output
  run_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run --detach \
    -o json --environment "${environment_id}" -- python -c 'import time; time.sleep(120)')"
  run_id="$(json_query "run create for axern ssh" 'json.load(sys.stdin)["run"]["id"]' "${run_output}")"
  [ -n "${run_id}" ] || {
    echo "axern run for ssh did not return a run id" >&2
    dump_logs
    exit 1
  }
  allocation_id="$(wait_for_running_run_allocation "${run_id}" "axern ssh run")"
  if ! ssh_output="$(printf 'echo axern-cli-ssh-ok\nexit\n' | "${AXERN_BIN}" --config "${cli_config_file}" --context axern-cli-e2e ssh --tty=false --ssh-option LogLevel=ERROR "${allocation_id}" 2>"${cli_error_output}" | tr -d '\r')"; then
    echo "axern ssh failed for allocation ${allocation_id}" >&2
    printf '%s\n' "${ssh_output}" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  if ! grep -q "axern-cli-ssh-ok" <<<"${ssh_output}"; then
    echo "axern ssh did not return expected output" >&2
    printf '%s\n' "${ssh_output}" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run cancel "${run_id}" -o json >"${cli_object_output}"
}
