#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C
umask 077

usage() {
  printf '%s\n' 'Usage: sudo bash prepare-qualification-runner.sh --disposable-guest arm64|amd64' \
    'Run only on a fresh, disposable Ubuntu 24.04 VM. Install the native host prerequisites first.'
}

fail() { printf '%s\n' "$*" >&2; exit 1; }

if [[ $# == 1 && $1 == --help ]]; then
  usage
  exit 0
fi
if [[ $# != 2 || $1 != --disposable-guest || ( $2 != arm64 && $2 != amd64 ) ]]; then
  usage >&2
  exit 2
fi

[[ $EUID == 0 ]] || fail 'Run as root on the disposable VM.'
case "$2/$(uname -m)/$(dpkg --print-architecture)" in
  arm64/aarch64/arm64 | amd64/x86_64/amd64) ;;
  *) fail 'Kernel and Ubuntu package architecture must match the requested native architecture.' ;;
esac
# shellcheck source=/dev/null
source /etc/os-release
[[ ${ID:-} == ubuntu && ${VERSION_ID:-} == 24.04 ]] || fail 'Use a disposable Ubuntu 24.04 VM.'

data=/var/lib/axern
sandbox_cidr=172.30.0.0/24
fixture_cidr=198.19.0.0/29

for parent in /etc /opt /run /var/lib; do
  [[ -d $parent && $(realpath -e -- "$parent") == "$parent" && $(stat -c %u -- "$parent") == 0 ]] ||
    fail "Unexpected host directory: $parent"
done
for path in "$data" /opt/axern /etc/axern /run/netns/axern-native-qual-fixture; do
  [[ ! -e $path && ! -L $path ]] || fail "Existing path on the qualification VM: $path"
done
if findmnt -n --mountpoint "$data" >/dev/null; then
  fail "Existing mount at $data"
fi

for tool in busybox mkfs.erofs ip iptables nft sysctl xfs_quota mkfs.xfs mount umount fallocate python3; do
  command -v "$tool" >/dev/null || fail "Missing native host prerequisite: $tool"
done
for tool in initdb postgres pg_isready createdb psql; do
  [[ -x /usr/lib/postgresql/16/bin/$tool ]] || fail "Missing PostgreSQL 16 tool: $tool"
done
[[ -d /run/systemd/system && $(stat -fc %T /sys/fs/cgroup) == cgroup2fs ]] ||
  fail 'A running systemd and cgroup v2 are required.'
read -r controllers < /sys/fs/cgroup/cgroup.controllers
for controller in cpu memory pids; do
  [[ " $controllers " == *" $controller "* ]] || fail "Missing cgroup v2 controller: $controller"
done

for link in sandbox0 axqhost0 axqpeer0; do
  if ip link show dev "$link" >/dev/null 2>&1; then
    fail "Existing qualification interface: $link"
  fi
done
ip -j -4 route show table all | python3 -I -c '
import ipaddress
import json
import socket
import sys

networks = [ipaddress.ip_network(value) for value in sys.argv[1:]]
for route in json.load(sys.stdin):
    destination = route.get("dst")
    if destination and destination != "default":
        if any(network.overlaps(ipaddress.ip_network(destination, strict=False)) for network in networks):
            sys.exit(f"Existing host route overlaps a qualification subnet: {destination}")
for port in (25432, 24000, 24001, 24002, 24100, 24010, 23001, 25000, 25080):
    with socket.socket() as listener:
        try:
            listener.bind(("127.0.0.1", port))
        except OSError as error:
            sys.exit(f"Native qualification port {port} is unavailable: {error.strerror}")
' "$sandbox_cidr" "$fixture_cidr"

read -r blocks block_size < <(stat -f -c '%a %S' /var/lib)
(( blocks * block_size >= 8 * 1024 ** 3 )) || fail 'Need at least 8 GiB available on /var/lib for qualification storage.'

forward_rules=$(iptables -w -S FORWARD)
docker_user_rules=
if grep -Eq -- '^-A FORWARD .*-(j|g) DOCKER-USER( |$)' <<< "$forward_rules"; then
  docker_user_rules=$(iptables -w -S DOCKER-USER)
fi
fixture_forwarding=false
if grep -Eq -- '^-P FORWARD DROP$|-(j|g) (DROP|REJECT)( |$)' <<< "$forward_rules"$'\n'"$docker_user_rules"; then
  fixture_forwarding=true
fi

outbound=(-i sandbox0 -o axqhost0 -s "$sandbox_cidr" -d "$fixture_cidr" -m comment --comment axern-qualification-fixture -j ACCEPT)
inbound=(-i axqhost0 -o sandbox0 -s "$fixture_cidr" -d "$sandbox_cidr" -m conntrack --ctstate "ESTABLISHED,RELATED" -m comment --comment axern-qualification-fixture -j ACCEPT)
if [[ $forward_rules == *axern-qualification-fixture* ]]; then
  fail 'Existing qualification forwarding rules on the VM.'
fi
forwarding=$(sysctl -n net.ipv4.ip_forward)
[[ $forwarding == 0 || $forwarding == 1 ]] || fail 'Unexpected IPv4 forwarding state.'

backing=
owns_data=false
mount_attempted=false
mounted=false
enabled_forwarding=false
added_inbound=false
added_outbound=false
cleanup_failure() {
  local result=$?
  trap - EXIT HUP INT TERM
  if $added_outbound; then iptables -w -D FORWARD "${outbound[@]}" || :; fi
  if $added_inbound; then iptables -w -D FORWARD "${inbound[@]}" || :; fi
  if $enabled_forwarding; then sysctl -w net.ipv4.ip_forward=0 || :; fi
  if $mounted || { $mount_attempted && findmnt -n --mountpoint "$data" >/dev/null; }; then
    if ! umount -- "$data"; then
      printf 'Could not unmount %s. Retaining its backing file in %s.\n' "$data" "$backing" >&2
      exit "$result"
    fi
  fi
  if $owns_data; then rmdir -- "$data" || :; fi
  if [[ -n $backing ]]; then
    rm -f -- "$backing/data.xfs" || :
    rmdir -- "$backing" || :
  fi
  exit "$result"
}
trap cleanup_failure EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# The new mountpoint reserves the fixed target before creating any backing data.
mkdir -m 0700 -- "$data"
owns_data=true
backing=$(mktemp -d /var/lib/axern-qualification.XXXXXXXX)
image=$backing/data.xfs
fallocate -l 6G -- "$image"
mkfs.xfs -K "$image"
mount_attempted=true
mount -t xfs -o loop,prjquota -- "$image" "$data"
mounted=true
allocated_blocks=$(stat -c %b -- "$image")
(( allocated_blocks * 512 >= 6 * 1024 ** 3 )) || fail 'The XFS image did not retain its 6 GiB allocation.'
findmnt --mountpoint "$data" -o TARGET,SOURCE,FSTYPE,OPTIONS,UUID
[[ $(findmnt -n -o FSTYPE --mountpoint "$data") == xfs ]] || fail 'Failed to mount the dedicated XFS filesystem.'
[[ -n $(findmnt -n -o UUID --mountpoint "$data") ]] || fail 'Missing XFS filesystem UUID.'
quota=$(xfs_quota -x -c 'state -p' "$data")
printf '%s\n' "$quota"
if ! grep -Eq '^[[:space:]]*Accounting:[[:space:]]+ON[[:space:]]*$' <<< "$quota" ||
  ! grep -Eq '^[[:space:]]*Enforcement:[[:space:]]+ON[[:space:]]*$' <<< "$quota"; then
  fail 'XFS project quota accounting and enforcement must both be active.'
fi

if [[ $forwarding == 0 ]]; then
  sysctl -w net.ipv4.ip_forward=1
  enabled_forwarding=true
fi
if $fixture_forwarding; then
  # Insert before Docker jumps and leave the separate Axern egress rules untouched.
  iptables -w -I FORWARD 1 "${inbound[@]}"
  added_inbound=true
  iptables -w -I FORWARD 1 "${outbound[@]}"
  added_outbound=true
  iptables -w -C FORWARD "${inbound[@]}"
  iptables -w -C FORWARD "${outbound[@]}"
fi
printf 'Prepared disposable qualification VM with backing file %s\n' "$image"
trap - EXIT HUP INT TERM
