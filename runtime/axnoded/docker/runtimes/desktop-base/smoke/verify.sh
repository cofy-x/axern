#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-axern/desktop-base-runtime:dev}"
CONTAINER_NAME="${CONTAINER_NAME:-axern-desktop-base-runtime-smoke}"

cleanup() {
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker image inspect "${IMAGE}" >/dev/null
cleanup
docker run -d --name "${CONTAINER_NAME}" "${IMAGE}" >/dev/null

for i in $(seq 1 30); do
  if docker exec "${CONTAINER_NAME}" bash -lc 'xdotool getdisplaygeometry >/dev/null 2>&1'; then
    break
  fi
  [ "${i}" -lt 30 ] || { echo "desktop-base display did not become ready" >&2; exit 1; }
  sleep 1
done

docker exec "${CONTAINER_NAME}" bash -lc 'set -e; command -v Xvfb; command -v chromium; command -v xdotool; python3 -c "import playwright"; chromium --version'
docker exec "${CONTAINER_NAME}" bash -lc 'set -e; xdotool mousemove 20 20; import -window root /tmp/desktop-smoke.png; test -s /tmp/desktop-smoke.png'
docker exec "${CONTAINER_NAME}" bash -lc 'set -e; python3 - <<"PY"
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True, executable_path="/usr/local/bin/chromium", args=["--no-sandbox"])
    page = browser.new_page()
    page.set_content("<title>axern-desktop-smoke</title><h1>ok</h1>")
    assert page.title() == "axern-desktop-smoke"
    browser.close()
PY'

! docker exec "${CONTAINER_NAME}" bash -lc 'command -v node' >/dev/null 2>&1
! docker exec "${CONTAINER_NAME}" bash -lc 'command -v go' >/dev/null 2>&1
! docker exec "${CONTAINER_NAME}" bash -lc 'command -v uv' >/dev/null 2>&1

echo "Axern desktop-base runtime smoke checks passed."
