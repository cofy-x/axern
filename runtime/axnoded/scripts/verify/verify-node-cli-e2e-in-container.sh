#!/usr/bin/env bash
set -euo pipefail

AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"

for _ in $(seq 1 40); do
  if [ -S "${AXNODED_SOCKET}" ] && curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${AXNODED_SOCKET}" ] || ! curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
  echo "axnoded readiness not ready" >&2
  exit 1
fi

health_output="$(axctl --address "${AXNODED_SOCKET}" node check)"
grep -q 'SERVING' <<<"${health_output}"

start_container() {
  local runtime_name="$1"
  local stdout_path="$2"
  local stderr_path="$3"
  local shell_command="${4:-sleep 300}"
  local output
  output="$(verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -stdout "${stdout_path}" \
    -stderr "${stderr_path}" \
    -shell-command "${shell_command}")"
  local sandbox_id
  sandbox_id="$(awk -F= '/^container_id=/{print $2}' <<<"${output}")"
  printf '%s\n' "${sandbox_id}"
}

assert_unary_exec() {
  local runtime_name="$1"
  local sandbox_id="$2"
  local stdout_capture="$3"
  local stderr_capture="$4"
  local expected_ok="ok-${runtime_name}"
  local expected_err="err-${runtime_name}"

  set +e
  local unary_stdout
  unary_stdout="$(axctl --address "${AXNODED_SOCKET}" sandbox exec "${sandbox_id}" -- /bin/sh -c "printf '${expected_ok}\n'; printf '${expected_err}\n' >&2; exit 7" 2>"${stderr_capture}")"
  local unary_code=$?
  set -e

  [ "${unary_code}" -eq 7 ] || {
    echo "unexpected unary exec exit code for ${runtime_name}: ${unary_code}" >&2
    exit 1
  }
  printf '%s' "${unary_stdout}" >"${stdout_capture}"
  grep -q "${expected_ok}" "${stdout_capture}"
  grep -q "${expected_err}" "${stderr_capture}"
}

assert_interactive_exec() {
  local runtime_name="$1"
  local sandbox_id="$2"
  local script_path="$3"
  local stdin_path="$4"
  local expected="hi-${runtime_name}"

  printf 'echo %s\nexit 0\n' "${expected}" >"${stdin_path}"
  script -qec "cat '${stdin_path}' | axctl --address '${AXNODED_SOCKET}' sandbox exec -it '${sandbox_id}' -- /bin/sh" "${script_path}" >/dev/null
  grep -q "${expected}" "${script_path}"
}

assert_resize_exec() {
  local sandbox_id="$1"
  local script_path="$2"

  script -qec "stty rows 33 cols 101; axctl --address '${AXNODED_SOCKET}' sandbox exec -it '${sandbox_id}' -- /bin/sh -lc 'stty size; exit 0'" "${script_path}" >/dev/null
  grep -q '33 101' "${script_path}"
}

assert_interactive_exec_has_no_default_timeout() {
  local runtime_name="$1"
  local sandbox_id="$2"
  local script_path="$3"
  local expected="linger-${runtime_name}"

  script -qec "axctl --address '${AXNODED_SOCKET}' sandbox exec -it '${sandbox_id}' -- /bin/sh -lc 'sleep 12; echo ${expected}; exit 0'" "${script_path}" >/dev/null
  grep -q "${expected}" "${script_path}"
}

assert_wait_respects_timeout() {
  local sandbox_id="$1"
  local timeout_capture="$2"

  set +e
  timeout 5s axctl --address "${AXNODED_SOCKET}" --timeout 1s sandbox wait "${sandbox_id}" >"${timeout_capture}" 2>&1
  local wait_code=$?
  set -e

  [ "${wait_code}" -ne 124 ] || {
    echo "wait did not honor CLI timeout for ${sandbox_id}" >&2
    exit 1
  }
  [ "${wait_code}" -ne 0 ] || {
    echo "wait unexpectedly succeeded for running sandbox ${sandbox_id}" >&2
    exit 1
  }
  grep -Eq 'DeadlineExceeded|context deadline exceeded' "${timeout_capture}"
}

assert_wait_reports_exit() {
  local sandbox_id="$1"
  local wait_capture="$2"
  local expected_exit="$3"

  axctl --address "${AXNODED_SOCKET}" sandbox wait "${sandbox_id}" >"${wait_capture}"
  grep -q "^Sandbox: ${sandbox_id}$" "${wait_capture}"
  grep -q "^Exit Code: ${expected_exit}$" "${wait_capture}"
}

assert_kill_reports_exit() {
  local sandbox_id="$1"
  local wait_capture="$2"
  local expected_exit="$3"

  axctl --address "${AXNODED_SOCKET}" sandbox kill "${sandbox_id}"
  assert_wait_reports_exit "${sandbox_id}" "${wait_capture}" "${expected_exit}"
}

assert_list_and_inspect() {
  local sandbox_id="$1"
  local list_capture="$2"
  local inspect_capture="$3"

  axctl --address "${AXNODED_SOCKET}" sandbox list >"${list_capture}"
  grep -q "${sandbox_id}" "${list_capture}"
  axctl --address "${AXNODED_SOCKET}" sandbox inspect "${sandbox_id}" >"${inspect_capture}"
  grep -q "^Sandbox: ${sandbox_id}$" "${inspect_capture}"
}

assert_diagnostics() {
  local sandbox_id="$1"
  local diagnostics_capture="$2"
  local diagnostics_json_capture="$3"

  axctl --address "${AXNODED_SOCKET}" sandbox diagnostics "${sandbox_id}" >"${diagnostics_capture}"
  grep -q "^Sandbox: ${sandbox_id}$" "${diagnostics_capture}"
  grep -q "^Ready: true$" "${diagnostics_capture}"
  grep -q "^User State: running$" "${diagnostics_capture}"
  grep -q "Capabilities: .*diagnostics" "${diagnostics_capture}"
  grep -q "Capabilities: .*process" "${diagnostics_capture}"
  grep -q "^Providers: " "${diagnostics_capture}"
  grep -q "^Processes: " "${diagnostics_capture}"
  grep -q "Provider Details:" "${diagnostics_capture}"

  axctl --address "${AXNODED_SOCKET}" sandbox diagnostics --json "${sandbox_id}" >"${diagnostics_json_capture}"
  jq -e '
    .protocolVersion == 1 and
    .ready == true and
    .detail == "full" and
    (.capabilities | index("diagnostics") != null) and
    (.capabilities | index("process") != null) and
    (.providers | length > 0) and
    (.processes != null)
  ' "${diagnostics_json_capture}" >/dev/null
}

runsc_output="$(start_container runsc /tmp/verify-cli-runsc.stdout /tmp/verify-cli-runsc.stderr "trap 'exit 99' TERM; sleep 300")"
runsc_id="${runsc_output}"
[ -n "${runsc_id}" ] || {
  echo "verify-cli did not return a runsc sandbox id" >&2
  exit 1
}

assert_list_and_inspect "${runsc_id}" /tmp/runsc.list.output /tmp/runsc.inspect.output
assert_diagnostics "${runsc_id}" /tmp/runsc.diagnostics.output /tmp/runsc.diagnostics.json
assert_unary_exec runsc "${runsc_id}" /tmp/runsc.exec.stdout /tmp/runsc.exec.stderr
assert_interactive_exec runsc "${runsc_id}" /tmp/runsc.exec.typescript /tmp/runsc.exec.stdin
assert_interactive_exec_has_no_default_timeout runsc "${runsc_id}" /tmp/runsc.exec.no-timeout.typescript
assert_wait_respects_timeout "${runsc_id}" /tmp/runsc.wait.timeout.output
assert_kill_reports_exit "${runsc_id}" /tmp/runsc.kill.wait.output 99

runsc_wait_output="$(start_container runsc /tmp/verify-cli-runsc-wait.stdout /tmp/verify-cli-runsc-wait.stderr 'sleep 1; exit 11')"
runsc_wait_id="${runsc_wait_output}"
[ -n "${runsc_wait_id}" ] || {
  echo "verify-cli did not return a second runsc sandbox id" >&2
  exit 1
}
assert_wait_reports_exit "${runsc_wait_id}" /tmp/runsc.wait.output 11
axctl --address "${AXNODED_SOCKET}" sandbox delete "${runsc_wait_id}"

runc_output="$(start_container runc /tmp/verify-cli-runc.stdout /tmp/verify-cli-runc.stderr)"
runc_id="${runc_output}"
[ -n "${runc_id}" ] || {
  echo "verify-cli did not return a runc sandbox id" >&2
  exit 1
}

assert_list_and_inspect "${runc_id}" /tmp/runc.list.output /tmp/runc.inspect.output
assert_diagnostics "${runc_id}" /tmp/runc.diagnostics.output /tmp/runc.diagnostics.json
assert_unary_exec runc "${runc_id}" /tmp/runc.exec.stdout /tmp/runc.exec.stderr
assert_interactive_exec runc "${runc_id}" /tmp/runc.exec.typescript /tmp/runc.exec.stdin
assert_resize_exec "${runc_id}" /tmp/runc.exec.resize.typescript
axctl --address "${AXNODED_SOCKET}" sandbox delete "${runc_id}"

runc_wait_output="$(start_container runc /tmp/verify-cli-runc-wait.stdout /tmp/verify-cli-runc-wait.stderr 'sleep 1; exit 13')"
runc_wait_id="${runc_wait_output}"
[ -n "${runc_wait_id}" ] || {
  echo "verify-cli did not return a second runc sandbox id" >&2
  exit 1
}
assert_wait_reports_exit "${runc_wait_id}" /tmp/runc.wait.output 13
axctl --address "${AXNODED_SOCKET}" sandbox delete "${runc_wait_id}"

echo "verify_node_cli_e2e_ok=true"
