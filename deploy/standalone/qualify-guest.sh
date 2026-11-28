#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
Usage: sudo qualify-guest.sh --disposable-guest ARCH ARCHIVE SHA256 SOURCE IMAGE STAGING_DIR OUTPUT_DIR
ARCH is arm64 or amd64. IMAGE is a trusted OCI manifest reference by digest with Python 3.
STAGING_DIR contains the archive revision's native_runtime.py and gvisor_payload.py.
Run on a fresh Ubuntu 24.04 guest with the native prerequisites and an empty XFS data mount.
EOF
}

fail() { printf '%s\n' "$*" >&2; exit 1; }

if [[ $# -eq 1 && $1 == --help ]]; then
    usage
    exit 0
fi

# Reserve the original stdout for one fixed phase value, including early exits.
exec 3>&1 1>&2
phase=input
namespace_created=false
link_created=false
start_attempted=false
fixture_pid=
cleanup() {
    local result=$?
    local cleanup_failed=0
    trap - EXIT HUP INT TERM
    if $start_attempted; then
        python3 -I "$installer" stop > "$output/stop.log" 2>&1 || cleanup_failed=1
        systemctl disable axern-native.target >> "$output/stop.log" 2>&1 || cleanup_failed=1
    fi
    if [[ -n $fixture_pid ]]; then
        if kill -0 "$fixture_pid" 2>/dev/null; then kill "$fixture_pid" || cleanup_failed=1; fi
        wait "$fixture_pid" || :
    fi
    if $namespace_created; then ip netns del "$namespace" || cleanup_failed=1; fi
    if $link_created && ip link show dev "$host_link" >/dev/null 2>&1; then
        ip link del "$host_link" || cleanup_failed=1
    fi
    if (( result == 0 )); then
        if (( cleanup_failed != 0 )); then
            result=1
            phase=cleanup
        else
            phase=complete
        fi
    fi
    printf '%s\n' "$phase" >&3 || :
    exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

if [[ $# -ne 8 || $1 != --disposable-guest ]]; then
    usage >&2
    exit 2
fi

architecture=$2
archive=$3
digest=$4
revision=$5
image=$6
staging=$7
output=$8
phase=privilege
[[ $EUID == 0 ]] || fail 'Run as root on the disposable guest.'
phase=input
[[ $digest =~ ^[0-9a-f]{64}$ && $revision =~ ^[0-9a-f]{40}$ ]] || fail 'Provide the trusted archive SHA-256 and full source revision.'
[[ $image =~ ^[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]] || fail 'Provide a digest-pinned OCI probe image.'
[[ $archive = /* && $staging = /* && $output = /* ]] || fail 'Use absolute guest paths.'
[[ -f $archive && -f $staging/native_runtime.py && -f $staging/gvisor_payload.py ]] || fail 'Missing archive or matching staging tools.'
phase=host_preflight
case "$architecture/$(uname -m)" in
    arm64/aarch64 | amd64/x86_64) ;;
    *) fail "Native guest architecture $(uname -m) does not match $architecture." ;;
esac
# shellcheck source=/dev/null
source /etc/os-release
[[ ${ID:-} == ubuntu && ${VERSION_ID:-} == 24.04 ]] || fail 'Use a disposable Ubuntu 24.04 guest.'
[[ ! -e /opt/axern && ! -L /opt/axern && ! -e /etc/axern && ! -L /etc/axern ]] || fail 'Use a guest without an existing Axern installation.'
phase=storage_preflight
data=/var/lib/axern
[[ $(findmnt --mountpoint "$data" -n -o FSTYPE) == xfs ]] || fail 'Mount a dedicated XFS filesystem at /var/lib/axern.'
mount_options=$(findmnt --mountpoint "$data" -n -o OPTIONS)
[[ ,$mount_options, =~ ,(pquota|prjquota), && -z $(ls -A -- "$data") ]] || fail 'Use an empty XFS data mount with project quotas enabled.'

phase=network_preflight
namespace=axern-native-qual-fixture
host_link=axqhost0
peer_link=axqpeer0
fixture=198.19.0.2
peer=198.19.0.3
[[ ! -e /run/netns/$namespace && ! -L /run/netns/$namespace ]] || fail 'The fixture namespace already exists.'
for link in "$host_link" "$peer_link"; do
    if ip link show dev "$link" >/dev/null 2>&1; then fail "Interface $link already exists."; fi
done
[[ -z $(ip -4 route show exact 198.19.0.0/29) ]] || fail 'The fixture subnet already has a route.'

phase=output_setup
umask 077
mkdir -- "$output"
printf '%s\n' "$(uname -srmo)" > "$output/kernel.txt"
phase=archive_verify
python3 -I "$staging/native_runtime.py" verify "$archive" --sha256 "$digest" > "$output/archive.json"
python3 -I - "$output/archive.json" "$architecture" "$revision" <<'PY'
import json
import pathlib
import sys

receipt = json.loads(pathlib.Path(sys.argv[1]).read_bytes())
if receipt.get("status") != "verified" or (receipt["architecture"], receipt["source_revision"]) != tuple(sys.argv[2:]):
    sys.exit("Verified archive architecture or source revision did not match the request.")
PY

phase=archive_stage
release=/opt/axern/releases/$digest
mkdir -m 0755 /opt/axern /opt/axern/releases
python3 -I "$staging/native_runtime.py" stage "$archive" --sha256 "$digest" --destination "$release" > "$output/stage.json"
installer=$release/libexec/axern/native_install.py

phase=fixture_setup
ip netns add "$namespace"
namespace_created=true
ip link add "$host_link" type veth peer name "$peer_link"
link_created=true
ip link set "$peer_link" netns "$namespace"
ip addr add 198.19.0.1/29 dev "$host_link"
ip link set "$host_link" up
ip -n "$namespace" link set lo up
ip -n "$namespace" addr add "$fixture/29" dev "$peer_link"
ip -n "$namespace" addr add "$peer/29" dev "$peer_link"
ip -n "$namespace" link set "$peer_link" up
ip -n "$namespace" route add default via 198.19.0.1
ip netns exec "$namespace" python3 -I "$release/libexec/axern/qualify_network.py" serve \
    --listen-ip "$fixture" --peer-ip "$peer" > "$output/fixture.log" 2>&1 &
fixture_pid=$!
deadline=$((SECONDS + 15))
until grep -Fq '"network_fixture_ready": true' "$output/fixture.log"; do
    if ! kill -0 "$fixture_pid" 2>/dev/null || ((SECONDS >= deadline)); then
        fail "Fixture did not start. See $output/fixture.log."
    fi
    sleep 0.2
done

phase=install
cat > "$output/native.json" <<'EOF'
{
  "node_id": "node-native-qualification",
  "memory_budget_bytes": 4294967296,
  "service_memory_budget_bytes": 1073741824,
  "memory_reserve_bytes": 1073741824,
  "storage_reserve_bytes": 1073741824,
  "sandbox_cidr": "172.30.0.1/24",
  "dns_nameservers": ["198.19.0.2"],
  "max_instances": 8,
  "workspace_capacity_bytes": 2147483648,
  "workspace_inode_capacity": 200000
}
EOF
unset AXERN_TLS_CERT AXERN_TLS_KEY
python3 -I "$installer" install --config "$output/native.json" > "$output/install.log" 2>&1
phase=startup
start_attempted=true
python3 -I "$installer" start > "$output/start.log" 2>&1 || {
    start_status=$?
    # Ask the still-running gateway before the EXIT trap stops the installation.
    # The packaged CLI serializes these three fields from protobuf enums.
    if ! (
        # shellcheck disable=SC1091
        source /etc/axern/client.env &&
            AXERN_TLS_SERVER_NAME=localhost timeout --kill-after=2s 8s "$release/bin/axern" \
                admin node capability snapshot node-native-qualification --output json
    ) 2>/dev/null | python3 -I -c '
import json, sys

rows = []
for item in json.load(sys.stdin)["snapshot"].get("observations", []):
    platform = item["key"].get("platform")
    if platform is not None:
        rows.append({"platform": platform, "state": item.get("state"), "reasonCode": item.get("reasonCode")})
print(json.dumps({"query": "ok", "platformObservations": rows}))
' > "$output/start-capabilities.json" 2>/dev/null; then
        printf '%s\n' '{"query":"unavailable"}' > "$output/start-capabilities.json"
    fi
    exit "$start_status"
}
phase=matrix
python3 -I "$release/libexec/axern/qualify_native.py" \
    --expected-source "$revision" --expected-archive-sha256 "$digest" \
    --image "$image" --fixture-ip "$fixture" --cases all --output-dir "$output/matrix"
