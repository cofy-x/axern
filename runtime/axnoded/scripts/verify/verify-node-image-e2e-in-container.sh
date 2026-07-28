#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
IMAGE_URL="${IMAGE_URL:?IMAGE_URL is required}"
EXPECT_MOUNT_TYPE="${EXPECT_MOUNT_TYPE:?EXPECT_MOUNT_TYPE is required}"
PROBE_PATH="${PROBE_PATH:?PROBE_PATH is required}"
VERIFY_ARGV_JSON="${VERIFY_ARGV_JSON:?VERIFY_ARGV_JSON is required}"
VERIFY_EXPECT_STDOUT="${VERIFY_EXPECT_STDOUT:-}"
VERIFY_EXPECT_STDERR="${VERIFY_EXPECT_STDERR:-}"
VERIFY_EXPECT_EXIT="${VERIFY_EXPECT_EXIT:-0}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"

for _ in $(seq 1 40); do
  if [ -S "${IMAGEMGR_SOCKET}" ] && [ -S "${AXNODED_SOCKET}" ] && curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${IMAGEMGR_SOCKET}" ] || ! [ -S "${AXNODED_SOCKET}" ] || ! curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
  echo "required sockets or axnoded readiness not ready" >&2
  exit 1
fi

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

get_imagemgr() {
  local path="$1"
  local body_file="$2"
  curl -sS --unix-socket "${IMAGEMGR_SOCKET}" \
    -o "${body_file}" \
    -w '%{http_code}' \
    "http://unix${path}"
}

payload_1="$(jq -cn --arg image_url "${IMAGE_URL}" '{image_url:$image_url,lease_id:"node-image-e2e-1",owner:"verification"}')"
payload_2="$(jq -cn --arg image_url "${IMAGE_URL}" '{image_url:$image_url,lease_id:"node-image-e2e-2",owner:"verification"}')"

mount_body_1="$(mktemp)"
status="$(post_imagemgr /oci_mount "${payload_1}" "${mount_body_1}")"
[ "${status}" = "200" ] || { cat "${mount_body_1}" >&2; exit 1; }
mount_path="$(jq -r '.mount_path' "${mount_body_1}")"
[ -n "${mount_path}" ] || { echo "empty mount_path returned from /oci_mount" >&2; exit 1; }
[ -d "${mount_path}" ] || { echo "mount_path is not a directory: ${mount_path}" >&2; exit 1; }
[ -e "${mount_path}${PROBE_PATH}" ] || { echo "probe path missing: ${mount_path}${PROBE_PATH}" >&2; exit 1; }

mount_body_2="$(mktemp)"
status="$(post_imagemgr /oci_mount "${payload_2}" "${mount_body_2}")"
[ "${status}" = "200" ] || { cat "${mount_body_2}" >&2; exit 1; }
mount_path_2="$(jq -r '.mount_path' "${mount_body_2}")"
[ "${mount_path}" = "${mount_path_2}" ] || { echo "mount path reuse mismatch: ${mount_path} != ${mount_path_2}" >&2; exit 1; }

details_body="$(mktemp)"
status="$(get_imagemgr /list_oci_mount_details "${details_body}")"
[ "${status}" = "200" ] || { cat "${details_body}" >&2; exit 1; }
detail_json="$(jq -c --arg image_url "${IMAGE_URL}" 'first(.mounts[]? | select(.image_url == $image_url)) // empty' "${details_body}")"
[ -n "${detail_json}" ] || { echo "no mounted image detail found for ${IMAGE_URL}" >&2; exit 1; }
detail_mount_type="$(jq -r '.mount_type' <<<"${detail_json}")"
detail_mount_path="$(jq -r '.mount_path' <<<"${detail_json}")"
[ "${detail_mount_type}" = "${EXPECT_MOUNT_TYPE}" ] || { echo "mount type mismatch: ${detail_mount_type} != ${EXPECT_MOUNT_TYPE}" >&2; exit 1; }
[ "${detail_mount_path}" = "${mount_path}" ] || { echo "mount path detail mismatch: ${detail_mount_path} != ${mount_path}" >&2; exit 1; }

umount_body="$(mktemp)"
status="$(post_imagemgr /oci_umount "${payload_1}" "${umount_body}")"
[ "${status}" = "200" ] || { cat "${umount_body}" >&2; exit 1; }

details_after_first_release="$(mktemp)"
status="$(get_imagemgr /list_oci_mount_details "${details_after_first_release}")"
[ "${status}" = "200" ] || { cat "${details_after_first_release}" >&2; exit 1; }
jq -e --arg image_url "${IMAGE_URL}" 'any(.mounts[]?; .image_url == $image_url and .lease_count == 1)' "${details_after_first_release}" >/dev/null || {
  echo "mount was not retained for the second lease" >&2
  exit 1
}

status="$(post_imagemgr /oci_umount "${payload_2}" "${umount_body}")"
[ "${status}" = "200" ] || { cat "${umount_body}" >&2; exit 1; }

for runtime_name in runsc runc; do
  details_before_runtime="$(mktemp)"
  status="$(get_imagemgr /list_oci_mount_details "${details_before_runtime}")"
  [ "${status}" = "200" ] || { cat "${details_before_runtime}" >&2; exit 1; }
  if jq -e --arg image_url "${IMAGE_URL}" 'any(.mounts[]?; .image_url == $image_url)' "${details_before_runtime}" >/dev/null; then
    echo "mount record still exists after direct imagemgr unmount for ${IMAGE_URL}" >&2
    exit 1
  fi

  /usr/local/bin/verify-smoke \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -runtime-id "${EXPECT_MOUNT_TYPE}-e2e-${runtime_name}" \
    -rootfs-src image \
    -image-url "${IMAGE_URL}" \
    -stdout "/tmp/${runtime_name}-${EXPECT_MOUNT_TYPE}.stdout" \
    -stderr "/tmp/${runtime_name}-${EXPECT_MOUNT_TYPE}.stderr" \
    -argv-json "${VERIFY_ARGV_JSON}" \
    -expect-stdout "${VERIFY_EXPECT_STDOUT}" \
    -expect-stderr "${VERIFY_EXPECT_STDERR}" \
    -expected-exit "${VERIFY_EXPECT_EXIT}"

  metrics_output="$(metricsz_fetch)"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
    "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=langruntime_lookup" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"
  metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_phase_duration_seconds" "histogram" "1" \
    "axern.phase=runtime_launch" "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"

  details_after_runtime="$(mktemp)"
  status="$(get_imagemgr /list_oci_mount_details "${details_after_runtime}")"
  [ "${status}" = "200" ] || { cat "${details_after_runtime}" >&2; exit 1; }
  if jq -e --arg image_url "${IMAGE_URL}" 'any(.mounts[]?; .image_url == $image_url)' "${details_after_runtime}" >/dev/null; then
    echo "mount record still exists after axnoded runtime cleanup for ${IMAGE_URL}" >&2
    exit 1
  fi
done

echo "verify_node_${EXPECT_MOUNT_TYPE}_e2e_ok=true"
