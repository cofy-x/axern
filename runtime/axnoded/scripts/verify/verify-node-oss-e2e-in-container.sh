#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
MINIO_ENDPOINT="${MINIO_ENDPOINT:-oss:9000}"
MINIO_URL="http://${MINIO_ENDPOINT}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY:-minioadmin}"
MINIO_BUCKET="${MINIO_BUCKET:-axnoded-e2e}"
MARKER_VALUE="oss-e2e-marker"
MARKER_PATH="/etc/oss-marker"
VALID_OBJECT="fixtures/rootfs.ext4"
BAD_AUTH_OBJECT="fixtures/rootfs-bad-auth.ext4"
INVALID_OBJECT="fixtures/not-ext4.txt"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"

for _ in $(seq 1 40); do
  if [ -S "${IMAGEMGR_SOCKET}" ] && [ -S "${AXNODED_SOCKET}" ] && curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if [ ! -S "${IMAGEMGR_SOCKET}" ] || [ ! -S "${AXNODED_SOCKET}" ]; then
  echo "required sockets not ready" >&2
  exit 1
fi

mc alias set local "${MINIO_URL}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" >/dev/null
mc mb --ignore-existing "local/${MINIO_BUCKET}" >/dev/null

fixture_root="$(mktemp -d)"
cleanup() {
  rm -rf "${fixture_root}"
}
trap cleanup EXIT

mkdir -p "${fixture_root}/bin" "${fixture_root}/etc" "${fixture_root}/dev" "${fixture_root}/proc" "${fixture_root}/sys" "${fixture_root}/tmp"
install -m 0755 /bin/busybox "${fixture_root}/bin/busybox"
ln -sf busybox "${fixture_root}/bin/sh"
ln -sf busybox "${fixture_root}/bin/cat"
ln -sf busybox "${fixture_root}/bin/echo"
printf '%s\n' "${MARKER_VALUE}" > "${fixture_root}${MARKER_PATH}"

ext4_image="/tmp/rootfs.ext4"
truncate -s 64M "${ext4_image}"
mkfs.ext4 -F -d "${fixture_root}" "${ext4_image}" >/tmp/mkfs-rootfs.log 2>&1
mc cp "${ext4_image}" "local/${MINIO_BUCKET}/${VALID_OBJECT}" >/dev/null
mc cp "${ext4_image}" "local/${MINIO_BUCKET}/${BAD_AUTH_OBJECT}" >/dev/null

invalid_image="/tmp/not-ext4.txt"
printf 'not an ext4 image\n' > "${invalid_image}"
mc cp "${invalid_image}" "local/${MINIO_BUCKET}/${INVALID_OBJECT}" >/dev/null

post_imagemgr() {
  local path="$1"
  local payload="$2"
  local body_file="$3"
  curl -sS --unix-socket "${IMAGEMGR_SOCKET}" \
    -o "${body_file}" \
    -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d "${payload}" \
    "http://unix${path}"
}

valid_payload_1="$(jq -cn \
  --arg endpoint "${MINIO_ENDPOINT}" \
  --arg bucket "${MINIO_BUCKET}" \
  --arg object "${VALID_OBJECT}" \
  --arg access_key_id "${MINIO_ACCESS_KEY}" \
  --arg access_key_secret "${MINIO_SECRET_KEY}" \
  '{endpoint:$endpoint,bucket:$bucket,object:$object,access_key_id:$access_key_id,access_key_secret:$access_key_secret,lease_id:"node-oss-e2e-1",owner:"verification"}')"
valid_payload_2="$(jq -c '.lease_id="node-oss-e2e-2"' <<<"${valid_payload_1}")"

mount_body_1="$(mktemp)"
status="$(post_imagemgr /oss_mount "${valid_payload_1}" "${mount_body_1}")"
[ "${status}" = "200" ] || { cat "${mount_body_1}" >&2; exit 1; }
mount_path="$(jq -r '.mount_path' "${mount_body_1}")"
[ -f "${mount_path}${MARKER_PATH}" ] || { echo "marker missing at ${mount_path}${MARKER_PATH}" >&2; exit 1; }
grep -qx "${MARKER_VALUE}" "${mount_path}${MARKER_PATH}"

mount_body_2="$(mktemp)"
status="$(post_imagemgr /oss_mount "${valid_payload_2}" "${mount_body_2}")"
[ "${status}" = "200" ] || { cat "${mount_body_2}" >&2; exit 1; }
mount_path_2="$(jq -r '.mount_path' "${mount_body_2}")"
[ "${mount_path}" = "${mount_path_2}" ] || { echo "mount reuse path mismatch" >&2; exit 1; }

umount_body_1="$(mktemp)"
status="$(post_imagemgr /oss_umount "$(jq -cn --arg endpoint "${MINIO_ENDPOINT}" --arg bucket "${MINIO_BUCKET}" --arg object "${VALID_OBJECT}" '{endpoint:$endpoint,bucket:$bucket,object:$object,lease_id:"node-oss-e2e-1"}')" "${umount_body_1}")"
[ "${status}" = "200" ] || { cat "${umount_body_1}" >&2; exit 1; }
[ -f "${mount_path}${MARKER_PATH}" ] || { echo "mount path disappeared after first unmount" >&2; exit 1; }

umount_body_2="$(mktemp)"
status="$(post_imagemgr /oss_umount "$(jq -cn --arg endpoint "${MINIO_ENDPOINT}" --arg bucket "${MINIO_BUCKET}" --arg object "${VALID_OBJECT}" '{endpoint:$endpoint,bucket:$bucket,object:$object,lease_id:"node-oss-e2e-2"}')" "${umount_body_2}")"
[ "${status}" = "200" ] || { cat "${umount_body_2}" >&2; exit 1; }
[ ! -e "${mount_path}" ] || { echo "mount path still exists after final unmount" >&2; exit 1; }

invalid_body="$(mktemp)"
invalid_payload="$(jq -cn \
  --arg endpoint "${MINIO_ENDPOINT}" \
  --arg bucket "${MINIO_BUCKET}" \
  --arg object "${INVALID_OBJECT}" \
  --arg access_key_id "${MINIO_ACCESS_KEY}" \
  --arg access_key_secret "${MINIO_SECRET_KEY}" \
  '{endpoint:$endpoint,bucket:$bucket,object:$object,access_key_id:$access_key_id,access_key_secret:$access_key_secret,lease_id:"node-oss-invalid",owner:"verification"}')"
status="$(post_imagemgr /oss_mount "${invalid_payload}" "${invalid_body}")"
[ "${status}" != "200" ] || { echo "non-ext4 mount unexpectedly succeeded" >&2; exit 1; }

bad_auth_body="$(mktemp)"
bad_auth_payload="$(jq -cn \
  --arg endpoint "${MINIO_ENDPOINT}" \
  --arg bucket "${MINIO_BUCKET}" \
  --arg object "${BAD_AUTH_OBJECT}" \
  --arg access_key_id "${MINIO_ACCESS_KEY}" \
  --arg access_key_secret "wrong-secret" \
  '{endpoint:$endpoint,bucket:$bucket,object:$object,access_key_id:$access_key_id,access_key_secret:$access_key_secret,lease_id:"node-oss-bad-auth",owner:"verification"}')"
status="$(post_imagemgr /oss_mount "${bad_auth_payload}" "${bad_auth_body}")"
[ "${status}" != "200" ] || { echo "bad auth mount unexpectedly succeeded" >&2; exit 1; }

for runtime_name in runsc runc; do
  /usr/local/bin/verify-smoke \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -runtime-id "oss-e2e-${runtime_name}" \
    -rootfs-src s3 \
    -s3-endpoint "${MINIO_ENDPOINT}" \
    -s3-bucket "${MINIO_BUCKET}" \
    -s3-object "${VALID_OBJECT}" \
    -s3-access-key-id "${MINIO_ACCESS_KEY}" \
    -s3-access-key-secret "${MINIO_SECRET_KEY}" \
    -stdout "/tmp/${runtime_name}-oss.stdout" \
    -stderr "/tmp/${runtime_name}-oss.stderr" \
    -command "cat ${MARKER_PATH}; echo generic-axnoded-err 1>&2; exit 0" \
    -expect-stdout "${MARKER_VALUE}"

  metrics_output="$(metricsz_fetch)"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
    "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=s3" "axern.result=ok"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=langruntime_lookup" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=s3" "axern.result=ok"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=runtime_launch" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=s3" "axern.result=ok"
done

bad_verify_stderr="$(mktemp)"
if /usr/local/bin/verify-smoke \
  -address "${AXNODED_SOCKET}" \
  -runtime runsc \
  -runtime-id "oss-e2e-bad-auth" \
  -rootfs-src s3 \
  -s3-endpoint "${MINIO_ENDPOINT}" \
  -s3-bucket "${MINIO_BUCKET}" \
  -s3-object "${BAD_AUTH_OBJECT}" \
  -s3-access-key-id "${MINIO_ACCESS_KEY}" \
  -s3-access-key-secret "wrong-secret" \
  -stdout /tmp/oss-bad-auth.stdout \
  -stderr /tmp/oss-bad-auth.stderr \
  -command "cat ${MARKER_PATH}; echo generic-axnoded-err 1>&2; exit 0" \
  -expect-stdout "${MARKER_VALUE}" \
  2>"${bad_verify_stderr}"; then
  echo "axnoded bad-auth verification unexpectedly succeeded" >&2
  exit 1
fi
grep -Eq 'failed to mount oss rootfs|access|credential|permission' "${bad_verify_stderr}"

echo "verify_node_oss_e2e_ok=true"
