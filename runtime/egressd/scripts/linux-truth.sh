#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Linux || "$(id -u)" -ne 0 ]]; then
  echo "egressd Linux truth requires root inside an isolated Linux environment" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
EGRESSD_BIN="${EGRESSD_BIN:-${ROOT_DIR}/bin/egressd}"
EGRESSDCTL_BIN="${EGRESSDCTL_BIN:-${ROOT_DIR}/bin/egressdctl}"
FIXTURES="${ROOT_DIR}/runtime/egressd/scripts/fixtures"
STATE_ROOT="$(mktemp -d /tmp/axern-egress-truth.XXXXXX)"
NETNS="axern-egress-truth"
HOST_IF="axet-host"
PEER_IF="axet-peer"
EGRESSD_PID=""
DNS_PID=""
DNS_V6_PID=""
HTTP_PID=""
TLS_PID=""
CIDR_HTTP_PID=""
DENIED_HTTP_PID=""
UDP_PID=""

cleanup() {
  [[ -n "${EGRESSD_PID}" ]] && kill "${EGRESSD_PID}" 2>/dev/null || true
  [[ -n "${DNS_PID}" ]] && kill "${DNS_PID}" 2>/dev/null || true
  [[ -n "${DNS_V6_PID}" ]] && kill "${DNS_V6_PID}" 2>/dev/null || true
  [[ -n "${HTTP_PID}" ]] && kill "${HTTP_PID}" 2>/dev/null || true
  [[ -n "${TLS_PID}" ]] && kill "${TLS_PID}" 2>/dev/null || true
  [[ -n "${CIDR_HTTP_PID}" ]] && kill "${CIDR_HTTP_PID}" 2>/dev/null || true
  [[ -n "${DENIED_HTTP_PID}" ]] && kill "${DENIED_HTTP_PID}" 2>/dev/null || true
  [[ -n "${UDP_PID}" ]] && kill "${UDP_PID}" 2>/dev/null || true
  ip netns del "${NETNS}" 2>/dev/null || true
  rm -rf "${STATE_ROOT}"
}
trap cleanup EXIT

diagnose() {
  echo "egressd Linux truth diagnostics:" >&2
  cat "${STATE_ROOT}/egressd.log" >&2 2>/dev/null || true
  nft list table inet axern_egress >&2 2>/dev/null || true
  ss -lnptu >&2 2>/dev/null || true
}
trap diagnose ERR

ip netns add "${NETNS}"
ip link add "${HOST_IF}" type veth peer name "${PEER_IF}"
ip link set "${PEER_IF}" netns "${NETNS}"
ip addr add 10.77.0.1/24 dev "${HOST_IF}"
ip addr add 93.184.216.34/32 dev "${HOST_IF}"
ip -6 addr add fd77::1/64 dev "${HOST_IF}"
ip -6 addr add 2001:db8::34/128 dev "${HOST_IF}"
ip link set "${HOST_IF}" up
ip netns exec "${NETNS}" ip link set lo up
ip netns exec "${NETNS}" ip addr add 10.77.0.2/24 dev "${PEER_IF}"
ip netns exec "${NETNS}" ip -6 addr add fd77::2/64 dev "${PEER_IF}"
ip netns exec "${NETNS}" ip link set "${PEER_IF}" up
ip netns exec "${NETNS}" ip route add default via 10.77.0.1
ip netns exec "${NETNS}" ip -6 route add default via fd77::1
sysctl -qw net.ipv4.ip_forward=1
sysctl -qw net.ipv6.conf.all.forwarding=1

python3 "${FIXTURES}/dns_server.py" --port 5353 --answer 93.184.216.34 &
DNS_PID=$!
python3 "${FIXTURES}/dns_server.py" --port 5356 --answer 2001:db8::34 &
DNS_V6_PID=$!
python3 -m http.server 80 --bind :: --directory "${STATE_ROOT}" >/dev/null 2>&1 &
HTTP_PID=$!
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=allowed.test -keyout "${STATE_ROOT}/tls.key" -out "${STATE_ROOT}/tls.crt" >/dev/null 2>&1
openssl s_server -quiet -accept 443 -cert "${STATE_ROOT}/tls.crt" -key "${STATE_ROOT}/tls.key" -www >/dev/null 2>&1 &
TLS_PID=$!
python3 -m http.server 8080 --bind 0.0.0.0 --directory "${STATE_ROOT}" >/dev/null 2>&1 &
CIDR_HTTP_PID=$!
python3 -m http.server 8081 --bind 0.0.0.0 --directory "${STATE_ROOT}" >/dev/null 2>&1 &
DENIED_HTTP_PID=$!
python3 "${FIXTURES}/udp_echo.py" --port 5354 &
UDP_PID=$!
mkdir -p "${STATE_ROOT}/state" "${STATE_ROOT}/run"
"${EGRESSD_BIN}" -root "${STATE_ROOT}/state" -socket "${STATE_ROOT}/run/egressd.sock" >"${STATE_ROOT}/egressd.log" 2>&1 &
EGRESSD_PID=$!
for _ in 1 2 3 4 5; do
  [[ -S "${STATE_ROOT}/run/egressd.sock" ]] && break
  sleep 1
done
[[ -S "${STATE_ROOT}/run/egressd.sock" ]] || { cat "${STATE_ROOT}/egressd.log" >&2; exit 1; }

ctl() { "${EGRESSDCTL_BIN}" -socket "${STATE_ROOT}/run/egressd.sock" "$@"; }
ns() { ip netns exec "${NETNS}" env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u NO_PROXY "$@"; }

if ctl -allocation no-upstream -attempt 1 -ip 10.77.0.2 -revision 1 -mode strict -domains allowed.test prepare >/dev/null 2>&1; then
  echo "strict domain policy started without an explicit DNS upstream" >&2; exit 1
fi
if ctl -allocation invalid-upstream -attempt 1 -ip 10.77.0.2 -revision 1 -mode dns-deny -domains denied.test -upstreams 127.0.0.1 prepare >/dev/null 2>&1; then
  echo "DNS policy started with an unusable loopback upstream" >&2; exit 1
fi
if ctl list | grep -Eq 'no-upstream|invalid-upstream'; then
  echo "rejected DNS policy changed durable state" >&2; exit 1
fi

ctl -allocation strict -attempt 1 -ip 10.77.0.2 -revision 1 -mode strict -domains allowed.test -upstreams 10.77.0.1:5353 prepare >/dev/null
ns python3 "${FIXTURES}/dns_query.py" allowed.test --server 10.77.0.1 --expect-rcode 0
ns python3 "${FIXTURES}/dns_query.py" allowed.test --server 10.77.0.1 --tcp --expect-rcode 0
ns curl --fail --silent --max-time 3 --resolve allowed.test:80:93.184.216.34 http://allowed.test/ >/dev/null
ns curl --insecure --fail --silent --max-time 3 --resolve allowed.test:443:93.184.216.34 https://allowed.test/ >/dev/null
if ns curl --fail --silent --max-time 2 --resolve denied.test:80:93.184.216.34 http://denied.test/ >/dev/null; then
  echo "strict policy allowed an unapproved Host on an authorized IP" >&2; exit 1
fi
if ns curl --insecure --fail --silent --max-time 2 --resolve denied.test:443:93.184.216.34 https://denied.test/ >/dev/null; then
  echo "strict policy allowed an unapproved TLS SNI on an authorized IP" >&2; exit 1
fi
if ns timeout 2 openssl s_client -noservername -connect 93.184.216.34:443 </dev/null 2>/dev/null | grep -q 'BEGIN CERTIFICATE'; then
  echo "strict policy allowed TLS without SNI" >&2; exit 1
fi
if ns curl --fail --silent --max-time 2 http://93.184.216.34/ >/dev/null; then
  echo "strict policy allowed a direct-IP HTTP request" >&2; exit 1
fi
if ns python3 "${FIXTURES}/dns_query.py" allowed.test --server 10.77.0.1 --port 5353 --expect-rcode 0 --timeout 1 >/dev/null 2>&1; then
  echo "strict policy allowed an alternate DNS destination" >&2; exit 1
fi

ctl -allocation strict -attempt 1 delete >/dev/null
ctl -allocation strict-v6 -attempt 1 -ip fd77::2 -revision 1 -mode strict -domains allowed-v6.test -upstreams 10.77.0.1:5356 prepare >/dev/null
if ! ns python3 "${FIXTURES}/dns_query.py" allowed-v6.test --server fd77::1 --aaaa --expect-rcode 0; then
  diagnose
  exit 1
fi
ns curl --fail --silent --max-time 3 --resolve 'allowed-v6.test:80:[2001:db8::34]' http://allowed-v6.test/ >/dev/null
if ns curl --fail --silent --max-time 2 'http://[2001:db8::34]/' >/dev/null; then
  echo "strict IPv6 policy allowed a direct-IP HTTP request" >&2; exit 1
fi
ctl -allocation strict-v6 -attempt 1 delete >/dev/null

ctl -allocation cidr -attempt 1 -ip 10.77.0.2 -revision 1 -mode strict -cidr-rules tcp@93.184.216.34/32@8080,udp@93.184.216.34/32@5354 prepare >/dev/null
ns curl --fail --silent --max-time 3 http://93.184.216.34:8080/ >/dev/null
if ns curl --fail --silent --max-time 2 http://93.184.216.34:8081/ >/dev/null; then
  echo "strict CIDR rule allowed an undeclared TCP port" >&2; exit 1
fi
ns python3 "${FIXTURES}/udp_probe.py" --address 93.184.216.34 --port 5354
ns python3 "${FIXTURES}/udp_probe.py" --address 93.184.216.34 --port 5355 --expect-timeout
ctl -allocation cidr -attempt 1 delete >/dev/null

ctl -allocation deny-all -attempt 1 -ip 10.77.0.2 -revision 1 -mode strict prepare >/dev/null
if ns curl --fail --silent --max-time 2 http://93.184.216.34:8080/ >/dev/null; then
  echo "strict deny-all allowed direct egress" >&2; exit 1
fi
ctl -allocation deny-all -attempt 1 delete >/dev/null

ctl -allocation dns-soft -attempt 1 -ip 10.77.0.2 -revision 1 -mode dns-deny -domains denied.test -upstreams 10.77.0.1:5353 prepare >/dev/null
ns python3 "${FIXTURES}/dns_query.py" denied.test --server 10.77.0.1 --expect-rcode 5
ns python3 "${FIXTURES}/dns_query.py" denied.test --server 10.77.0.1 --tcp --expect-rcode 5
ns python3 "${FIXTURES}/dns_query.py" allowed.test --server 10.77.0.1 --expect-rcode 0
ns python3 "${FIXTURES}/dns_query.py" allowed.test --server 10.77.0.1 --tcp --expect-rcode 0
ns curl --fail --silent --max-time 3 http://93.184.216.34/ >/dev/null
ctl -allocation dns-soft -attempt 1 delete >/dev/null

ctl -allocation recovery -attempt 7 -ip 10.77.0.2 -revision 3 -mode strict -domains allowed.test -upstreams 10.77.0.1:5353 prepare >/dev/null
REDIRECT_HANDLE="$(nft -a list chain inet axern_egress proxy_redirect | awk '/10\.77\.0\.2 tcp dport 80 .*redirect to :1080/ { print $NF; exit }')"
if [[ -z "${REDIRECT_HANDLE}" ]]; then
  echo "could not identify the strict HTTP interception rule" >&2; exit 1
fi
nft delete rule inet axern_egress proxy_redirect handle "${REDIRECT_HANDLE}"
HEALTH_JSON="$(ctl health)"
if ! grep -q 'EGRESS_MANAGER_STATUS_ERROR' <<<"${HEALTH_JSON}" || ! grep -q 'nft ruleset proof mismatch' <<<"${HEALTH_JSON}"; then
  echo "egressd did not detect a missing managed nft rule" >&2; exit 1
fi
if ns curl --fail --silent --max-time 2 --resolve allowed.test:80:93.184.216.34 http://allowed.test/ >/dev/null; then
  echo "strict traffic failed open after an interception rule was deleted" >&2; exit 1
fi
kill "${EGRESSD_PID}"
wait "${EGRESSD_PID}" 2>/dev/null || true
EGRESSD_PID=""
"${EGRESSD_BIN}" -root "${STATE_ROOT}/state" -socket "${STATE_ROOT}/run/egressd.sock" >>"${STATE_ROOT}/egressd.log" 2>&1 &
EGRESSD_PID=$!
for _ in 1 2 3 4 5; do
  ctl health >/dev/null 2>&1 && break
  sleep 1
done
if ! ctl list | grep -q 'recovery'; then
  echo "persisted policy was not restored after egressd restart" >&2; exit 1
fi
if ! nft list chain inet axern_egress forward | grep -q "10.77.0.2 drop"; then
  echo "persisted policy did not restore its dataplane rules" >&2; exit 1
fi
if ! ctl health | grep -q 'EGRESS_MANAGER_STATUS_OK'; then
  echo "restored nft rules did not recover enforcement health" >&2; exit 1
fi
ctl reconcile | grep -Eq '"deleted_count":[[:space:]]+1'
if ctl list | grep -q '"allocation_id"'; then
  echo "orphan policy survived reconcile" >&2; exit 1
fi

ctl -allocation crash -attempt 1 -ip 10.77.0.2 -revision 1 -mode strict -domains allowed.test -upstreams 10.77.0.1:5353 prepare >/dev/null
kill "${EGRESSD_PID}"
wait "${EGRESSD_PID}" 2>/dev/null || true
EGRESSD_PID=""
if ! nft list chain inet axern_egress forward | grep -q "10.77.0.2 drop"; then
  echo "strict fail-closed rule disappeared after egressd crash" >&2; exit 1
fi
if ss -lnt | grep -Eq ':(1080|1443)[[:space:]]'; then
  echo "egressd proxy listener survived daemon exit" >&2; exit 1
fi
if ns curl --fail --silent --max-time 2 --resolve allowed.test:80:93.184.216.34 http://allowed.test/ >/dev/null; then
  echo "strict traffic survived an egressd crash" >&2; exit 1
fi
if grep -Eq 'allowed\.test|denied\.test|93\.184\.216\.34|2001:db8::34' "${STATE_ROOT}/egressd.log"; then
  echo "default egressd logs exposed a DNS name, Host, SNI, or destination IP" >&2; exit 1
fi

echo "egressd_linux_truth_ok=true"
