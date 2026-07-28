#!/usr/bin/env bash
set -euo pipefail

cd /workspace/runtime/axnoded

tmpdir="$(mktemp -d /tmp/axern-sandboxd-e2e.XXXXXX)"
cleanup() {
  if [ -n "${daemon_pid:-}" ]; then
    kill "${daemon_pid}" >/dev/null 2>&1 || true
    wait "${daemon_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

go test ./cmd/axern-sandboxd/...
go build -o "${tmpdir}/axern-sandboxd" ./cmd/axern-sandboxd

cat >"${tmpdir}/unixget.go" <<'GO'
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 3 || len(os.Args) > 5 {
		fmt.Fprintln(os.Stderr, "usage: unixget <socket> <path> [method] [body]")
		os.Exit(2)
	}
	socketPath := os.Args[1]
	requestPath := os.Args[2]
	method := http.MethodGet
	if len(os.Args) >= 4 {
		method = os.Args[3]
	}
	var requestBody io.Reader
	if len(os.Args) == 5 {
		requestBody = strings.NewReader(os.Args[4])
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	req, err := http.NewRequest(method, "http://unix"+requestPath, requestBody)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d\n%s", resp.StatusCode, responseBody)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		os.Exit(1)
	}
}
GO

unix_get() {
  go run "${tmpdir}/unixget.go" "$@"
}

unix_post() {
  go run "${tmpdir}/unixget.go" "$1" "$2" POST "$3"
}

wait_for_socket() {
  local socket_path="$1"
  local deadline=$((SECONDS + 10))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if [ -S "${socket_path}" ]; then
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for socket ${socket_path}" >&2
  return 1
}

assert_status_contains() {
  local socket_path="$1"
  local path="$2"
  local expected="$3"
  local output
  output="$(unix_get "${socket_path}" "${path}")"
  grep -q "${expected}" <<<"${output}" || {
    echo "expected ${path} output to contain ${expected}" >&2
    echo "${output}" >&2
    exit 1
  }
}

assert_exit_code() {
  local expected="$1"
  shift
  set +e
  "$@"
  local code=$?
  set -e
  if [ "${code}" -ne "${expected}" ]; then
    echo "unexpected exit code: got ${code}, want ${expected}: $*" >&2
    exit 1
  fi
}

assert_exit_code_silent() {
  local expected="$1"
  shift
  local output
  set +e
  output="$("$@" 2>&1)"
  local code=$?
  set -e
  if [ "${code}" -ne "${expected}" ]; then
    echo "unexpected exit code: got ${code}, want ${expected}: $*" >&2
    echo "${output}" >&2
    exit 1
  fi
}

socket_path="${tmpdir}/sandboxd.sock"
"${tmpdir}/axern-sandboxd" --socket "${socket_path}" >"${tmpdir}/not-started.out" 2>"${tmpdir}/not-started.err" &
daemon_pid=$!
wait_for_socket "${socket_path}"
assert_status_contains "${socket_path}" /healthz '"status":"ok"'
assert_status_contains "${socket_path}" /readyz '"ready":true'
assert_status_contains "${socket_path}" /capabilities '"protocolVersion":1'
assert_status_contains "${socket_path}" /capabilities '"supervisor"'
assert_status_contains "${socket_path}" /capabilities '"providers"'
assert_status_contains "${socket_path}" /capabilities '"name":"computer_use"'
assert_status_contains "${socket_path}" /capabilities '"available":false'
assert_status_contains "${socket_path}" /diagnostics '"providerSummary"'
assert_status_contains "${socket_path}" /diagnostics '"processSummary"'
assert_status_contains "${socket_path}" '/diagnostics?detail=full' '"mounts"'
assert_status_contains "${socket_path}" '/diagnostics?detail=full' '"ports"'
assert_status_contains "${socket_path}" '/diagnostics?detail=full' '"fileLimits"'
assert_status_contains "${socket_path}" '/diagnostics?detail=full' '"computerUse"'
assert_status_contains "${socket_path}" /status '"state":"not_started"'
assert_exit_code_silent 1 unix_post "${socket_path}" /processes '{"args":["/bin/true"],"unexpected":true}'
kill -TERM "${daemon_pid}"
wait "${daemon_pid}"
daemon_pid=""

entrypoint_exit7="${tmpdir}/exit7.json"
printf '{"args":["/bin/sh","-c","exit 7"]}\n' >"${entrypoint_exit7}"
assert_exit_code 7 "${tmpdir}/axern-sandboxd" --socket "${tmpdir}/exit7.sock" --entrypoint-json "${entrypoint_exit7}"

entrypoint_missing="${tmpdir}/missing.json"
printf '{"args":["/tmp/axern-sandboxd-missing-binary"]}\n' >"${entrypoint_missing}"
assert_exit_code 127 "${tmpdir}/axern-sandboxd" --socket "${tmpdir}/missing.sock" --entrypoint-json "${entrypoint_missing}"

entrypoint_signal="${tmpdir}/signal.json"
printf '{"args":["/bin/sh","-c","while true; do sleep 1; done"]}\n' >"${entrypoint_signal}"
"${tmpdir}/axern-sandboxd" --socket "${tmpdir}/signal.sock" --entrypoint-json "${entrypoint_signal}" >"${tmpdir}/signal.out" 2>"${tmpdir}/signal.err" &
daemon_pid=$!
wait_for_socket "${tmpdir}/signal.sock"
assert_status_contains "${tmpdir}/signal.sock" /status '"state":"running"'
set +e
kill -TERM "${daemon_pid}"
wait "${daemon_pid}"
signal_code=$?
set -e
daemon_pid=""
if [ "${signal_code}" -ne 143 ]; then
  echo "unexpected signal forwarding exit code: got ${signal_code}, want 143" >&2
  exit 1
fi

entrypoint_fast="${tmpdir}/fast.json"
printf '{"args":["/bin/sh","-c","exit 23"]}\n' >"${entrypoint_fast}"
for _ in $(seq 1 25); do
  assert_exit_code 23 "${tmpdir}/axern-sandboxd" --socket "${tmpdir}/fast.sock" --entrypoint-json "${entrypoint_fast}"
done

echo "verify_sandboxd_e2e_ok=true"
