#!/usr/bin/env bash

verify_ssh_gateway() {
  ssh_service_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create \
    -o json \
    --environment-id "${environment_id}" \
    --replicas 1 \
    --argv python \
    --argv -u \
    --argv -c \
    --argv 'import http.server, socketserver; socketserver.TCPServer.allow_reuse_address=True; socketserver.TCPServer(("", 8080), http.server.SimpleHTTPRequestHandler).serve_forever()' \
    --readiness-http-port 8080 \
    --readiness-http-path / \
    --readiness-period 1s \
    --readiness-timeout 1s)"
  ssh_service_id="$(json_query "service create for axern ssh" 'json.load(sys.stdin)["service"]["id"]' "${ssh_service_output}")"
  [ -n "${ssh_service_id}" ] || {
    echo "axern service create for ssh did not return a service id" >&2
    dump_logs
    exit 1
  }
  ssh_allocation_id="$(wait_for_ready_service_allocation "${ssh_service_id}" "axern ssh service")"
  if ! ssh_output="$(printf 'echo axern-cli-ssh-ok\nexit\n' | "${AXERN_BIN}" --config "${cli_config_file}" --context axern-cli-e2e ssh --tty=false --ssh-option LogLevel=ERROR "${ssh_allocation_id}" 2>"${cli_error_output}" | tr -d '\r')"; then
    echo "axern ssh failed for allocation ${ssh_allocation_id}" >&2
    echo "--- ssh stdout ---" >&2
    printf '%s\n' "${ssh_output}" >&2
    echo "--- ssh stderr ---" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  if ! grep -q "axern-cli-ssh-ok" <<<"${ssh_output}"; then
    echo "axern ssh did not return expected output" >&2
    echo "--- ssh stdout ---" >&2
    printf '%s\n' "${ssh_output}" >&2
    echo "--- ssh stderr ---" >&2
    cat "${cli_error_output}" >&2 || true
    dump_logs
    exit 1
  fi
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service delete "${ssh_service_id}" -o json >"${cli_object_output}"
}
