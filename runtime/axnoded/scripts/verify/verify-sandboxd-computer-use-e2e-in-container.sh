#!/usr/bin/env bash
set -euo pipefail

cd /workspace/runtime/axnoded

tmpdir="$(mktemp -d /tmp/axern-sandboxd-computer-use-e2e.XXXXXX)"
cleanup() {
  if [ -n "${daemon_pid:-}" ]; then
    kill "${daemon_pid}" >/dev/null 2>&1 || true
    wait "${daemon_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

go build -o "${tmpdir}/axern-sandboxd" ./cmd/axern-sandboxd

cat >"${tmpdir}/check.go" <<'GO'
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 7 {
		fmt.Fprintln(os.Stderr, "usage: check <socket> <mouse-file> <keyboard-file> <browser-open-file> <browser-close-file> <xdotool-log>")
		os.Exit(2)
	}
	socketPath, mousePath, keyboardPath := os.Args[1], os.Args[2], os.Args[3]
	browserOpenPath, browserClosePath := os.Args[4], os.Args[5]
	xdotoolLogPath := os.Args[6]
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	mustJSON(client, http.MethodGet, "/capabilities", nil, func(body []byte) {
		mustContain(body, `"computer_use"`)
		mustContain(body, `"browser"`)
		mustContain(body, `"available":true`)
	})
	mustJSON(client, http.MethodGet, "/browser/status", nil, func(body []byte) {
		mustContain(body, `"available":true`)
		mustContain(body, `"running":false`)
	})
	mustJSON(client, http.MethodPost, "/browser/open", []byte(`{"url":"https://example.com"}`), func(body []byte) {
		mustContain(body, `"available":true`)
		mustContain(body, `"running":true`)
	})
	mustFile(browserOpenPath, "https://example.com")
	mustJSON(client, http.MethodPost, "/browser/navigate", []byte(`{"url":"https://example.org"}`), func(body []byte) {
		mustContain(body, `"running":true`)
		mustContain(body, `"url":"https://example.org"`)
	})
	mustJSON(client, http.MethodPost, "/browser/resize", []byte(`{"width":1024,"height":768}`), nil)
	mustJSON(client, http.MethodPost, "/browser/click", []byte(`{"x":11,"y":22}`), nil)
	mustJSON(client, http.MethodPost, "/browser/type", []byte(`{"text":"hello","delayMs":1}`), nil)
	mustJSON(client, http.MethodPost, "/browser/wait", []byte(`{"timeoutMs":1}`), nil)
	mustFileContains(xdotoolLogPath, "getactivewindow windowsize 1024 768")
	mustFileContains(xdotoolLogPath, "mousemove 11 22 click 1")
	mustFileContains(xdotoolLogPath, "type --delay 1 -- hello")
	mustJSON(client, http.MethodPost, "/browser/close", nil, func(body []byte) {
		mustContain(body, `"available":true`)
		mustContain(body, `"running":false`)
	})
	mustFile(browserClosePath, "closed")
	mustJSON(client, http.MethodGet, "/computer-use/status", nil, func(body []byte) {
		mustContain(body, `"available":true`)
		mustContain(body, `"display":":99"`)
		for _, name := range []string{"display_env", "screenshot_backend", "display_backend", "input_backend", "display_server"} {
			mustContain(body, `"name":"`+name+`"`)
		}
	})
	mustJSON(client, http.MethodGet, "/computer-use/display", nil, func(body []byte) {
		mustContain(body, `"width":1280`)
		mustContain(body, `"height":720`)
	})
	resp, err := client.Get("http://unix/computer-use/screenshot")
	must(err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	must(err)
	if resp.StatusCode != http.StatusOK {
		fail("screenshot status=%d body=%s", resp.StatusCode, string(body))
	}
	if len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		fail("screenshot is not PNG: %q", body)
	}
	mustJSON(client, http.MethodPost, "/computer-use/mouse", []byte(`{"x":7,"y":9,"button":"1"}`), nil)
	mustJSON(client, http.MethodPost, "/computer-use/keyboard", []byte(`{"text":"hello"}`), nil)
	mustFile(mousePath, "7:9:1:click")
	mustFile(keyboardPath, "hello:")
}

func mustJSON(client *http.Client, method string, path string, body []byte, inspect func([]byte)) {
	var input io.Reader
	if body != nil {
		input = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://unix"+path, input)
	must(err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	must(err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	must(err)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fail("%s status=%d body=%s", path, resp.StatusCode, string(data))
	}
	if inspect != nil {
		inspect(data)
	}
	var discard any
	_ = json.Unmarshal(data, &discard)
}

func mustContain(body []byte, needle string) {
	if !bytes.Contains(body, []byte(needle)) {
		fail("missing %q in %s", needle, string(body))
	}
}

func mustFile(path string, want string) {
	data, err := os.ReadFile(path)
	must(err)
	if string(data) != want {
		fail("%s=%q want %q", path, string(data), want)
	}
}

func mustFileContains(path string, want string) {
	data, err := os.ReadFile(path)
	must(err)
	if !bytes.Contains(data, []byte(want)) {
		fail("%s missing %q in %q", path, want, string(data))
	}
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
GO

socket_path="${tmpdir}/sandboxd.sock"
mouse_path="${tmpdir}/mouse"
keyboard_path="${tmpdir}/keyboard"
browser_open_path="${tmpdir}/browser-open"
browser_close_path="${tmpdir}/browser-close"
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
AXERN_SANDBOXD_BROWSER_CLOSE_CMD="printf closed >${browser_close_path}" \
"${tmpdir}/axern-sandboxd" --socket "${socket_path}" >"${tmpdir}/sandboxd.out" 2>"${tmpdir}/sandboxd.err" &
daemon_pid=$!

deadline=$((SECONDS + 10))
while [ "${SECONDS}" -lt "${deadline}" ]; do
  if [ -S "${socket_path}" ]; then
    break
  fi
  sleep 0.1
done
test -S "${socket_path}"

go run "${tmpdir}/check.go" "${socket_path}" "${mouse_path}" "${keyboard_path}" "${browser_open_path}" "${browser_close_path}" "${xdotool_log_path}"

kill -TERM "${daemon_pid}"
wait "${daemon_pid}" >/dev/null 2>&1 || true
daemon_pid=""

echo "verify_sandboxd_computer_use_e2e_ok=true"
