#!/usr/bin/env bash
set -euo pipefail

cd /workspace/runtime/axnoded

tmpdir="$(mktemp -d /tmp/axern-sandboxd-provider-e2e.XXXXXX)"
cleanup() {
  if [ -n "${daemon_pid:-}" ]; then
    kill "${daemon_pid}" >/dev/null 2>&1 || true
    wait "${daemon_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

go build -o "${tmpdir}/axern-sandboxd" ./cmd/axern-sandboxd

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

socket_path="${tmpdir}/baseline.sock"
DISPLAY= \
WAYLAND_DISPLAY= \
AXERN_SANDBOXD_BROWSER_CMD= \
AXERN_SANDBOXD_BROWSER_OPEN_CMD= \
PATH="/usr/bin:/bin" \
"${tmpdir}/axern-sandboxd" --socket "${socket_path}" >"${tmpdir}/baseline.out" 2>"${tmpdir}/baseline.err" &
daemon_pid=$!
wait_for_socket "${socket_path}"
go run ./cmd/verify-sandboxd-provider --mode baseline --socket "${socket_path}"
kill -TERM "${daemon_pid}"
wait "${daemon_pid}" >/dev/null 2>&1 || true
daemon_pid=""

optional_socket_path="${tmpdir}/optional.sock"
mouse_path="${tmpdir}/mouse"
keyboard_path="${tmpdir}/keyboard"
browser_open_path="${tmpdir}/browser-open"
xdotool_log_path="${tmpdir}/xdotool.log"
mkdir -p "${tmpdir}/bin"
cat >"${tmpdir}/bin/xdotool" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AXERN_XDOTOOL_LOG}"
SH
chmod +x "${tmpdir}/bin/xdotool"
png_base64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

DISPLAY=:99 \
PATH="${tmpdir}/bin:${PATH}" \
AXERN_XDOTOOL_LOG="${xdotool_log_path}" \
AXERN_SANDBOXD_SCREENSHOT_CMD="printf %s ${png_base64} | base64 -d" \
AXERN_SANDBOXD_DISPLAY_CMD="printf '1280 720'" \
AXERN_SANDBOXD_MOUSE_CMD="printf '%s:%s:%s:%s' \"\$AXERN_COMPUTER_USE_MOUSE_X\" \"\$AXERN_COMPUTER_USE_MOUSE_Y\" \"\$AXERN_COMPUTER_USE_MOUSE_BUTTON\" \"\$AXERN_COMPUTER_USE_MOUSE_ACTION\" >${mouse_path}" \
AXERN_SANDBOXD_KEYBOARD_CMD="printf '%s:%s' \"\$AXERN_COMPUTER_USE_KEYBOARD_TEXT\" \"\$AXERN_COMPUTER_USE_KEYBOARD_KEY\" >${keyboard_path}" \
AXERN_SANDBOXD_BROWSER_OPEN_CMD="printf '%s' \"\$AXERN_BROWSER_URL\" >${browser_open_path}" \
"${tmpdir}/axern-sandboxd" --socket "${optional_socket_path}" >"${tmpdir}/optional.out" 2>"${tmpdir}/optional.err" &
daemon_pid=$!
wait_for_socket "${optional_socket_path}"
go run ./cmd/verify-sandboxd-provider \
  --mode optional \
  --socket "${optional_socket_path}" \
  --mouse-file "${mouse_path}" \
  --keyboard-file "${keyboard_path}" \
  --browser-open-file "${browser_open_path}"
kill -TERM "${daemon_pid}"
wait "${daemon_pid}" >/dev/null 2>&1 || true
daemon_pid=""

echo "verify_sandboxd_provider_contract_e2e_ok=true"
