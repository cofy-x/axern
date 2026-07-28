#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
export PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
PLAYWRIGHT_VERSION="${PLAYWRIGHT_VERSION:-1.59.0}"

apt-get update
apt-get install -y --no-install-recommends \
  dbus-x11 \
  fluxbox \
  fonts-dejavu \
  imagemagick \
  python3 \
  python3-pip \
  x11-utils \
  x11-xserver-utils \
  xdotool \
  xvfb

python3 -m pip install --break-system-packages --no-cache-dir "playwright==${PLAYWRIGHT_VERSION}"
python3 -m playwright install --with-deps --no-shell chromium

browser="$(python3 - <<'PY'
from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    print(p.chromium.executable_path)
PY
)"
if [ -z "${browser}" ] || [ ! -x "${browser}" ]; then
  browser="$(find /ms-playwright \
    \( -path '*/chrome-linux/chrome' -o -path '*/chrome-linux64/chrome' \) \
    -type f | sort | tail -n 1)"
fi

install -d -m 0755 /usr/local/bin
cat >/usr/local/bin/chromium <<EOF
#!/usr/bin/env bash
set -euo pipefail

browser="${browser}"
if [ -z "\${browser}" ] || [ ! -x "\${browser}" ]; then
  echo "Playwright Chromium executable not found under /ms-playwright" >&2
  exit 127
fi
exec "\${browser}" "\$@"
EOF
chmod 0755 /usr/local/bin/chromium

chromium --version
chmod -R a+rX /ms-playwright
rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
