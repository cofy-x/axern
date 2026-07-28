#!/usr/bin/env bash
set -euo pipefail

retry_local_http() {
  local attempt=0
  local body=""
  while [ "${attempt}" -lt 30 ]; do
    attempt=$((attempt + 1))
    if body="$(curl -fsS --connect-timeout 2 --max-time 5 http://127.0.0.1:80/ 2>/tmp/axern-server-base-curl.err)" &&
      [ "${body}" = "axern-server-base-ok" ]; then
      return 0
    fi
    sleep 1
  done
  echo "server-base local HTTP check failed after ${attempt} attempts" >&2
  if [ -s /tmp/axern-server-base-curl.err ]; then
    cat /tmp/axern-server-base-curl.err >&2
  fi
  return 1
}

pgrep -x supervisord >/dev/null
pgrep -x sshd >/dev/null
pgrep -x nginx >/dev/null
sshd -T | grep -i "^passwordauthentication no$" >/dev/null
sshd -T | grep -i "^kbdinteractiveauthentication no$" >/dev/null
sshd -T | grep -i "^permitrootlogin no$" >/dev/null
id axern >/dev/null
sudo -u axern id >/dev/null
grep -Eq '^axern[[:space:]]+ALL=\(ALL\)[[:space:]]+NOPASSWD:ALL$' /etc/sudoers.d/91-axern
retry_local_http
! command -v node >/dev/null
! command -v go >/dev/null
! command -v poetry >/dev/null
! command -v pipx >/dev/null
ip -V >/dev/null
ping -V >/dev/null
ps -p 1 >/dev/null
echo server-base-default-entrypoint-ok
