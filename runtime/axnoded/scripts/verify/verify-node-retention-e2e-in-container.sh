#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
IMAGE_URL="${IMAGE_URL:?IMAGE_URL is required}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
AXNODED_IDLE_RUNTIME_RETENTION_TTL="${AXNODED_IDLE_RUNTIME_RETENTION_TTL:-5s}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"

container_id=""

cleanup() {
  if [ -n "${container_id}" ]; then
    axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

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

fetch_axnoded_inventory() {
  local body_file="$1"
  curl -fsS "http://127.0.0.1:23001/inventoryz" >"${body_file}"
}

fetch_imagemgr_details() {
  local body_file="$1"
  curl -fsS --unix-socket "${IMAGEMGR_SOCKET}" http://unix/list_oci_mount_details >"${body_file}"
}

fetch_metrics() {
  metricsz_fetch
}

wait_for_jq() {
  local description="$1"
  local file_path="$2"
  local max_wait="$3"
  local filter="$4"
  shift 4
  local jq_args=("$@")
  local started_at="${SECONDS}"

  while [ $((SECONDS - started_at)) -lt "${max_wait}" ]; do
    if jq -e "${jq_args[@]}" "${filter}" "${file_path}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    case "${file_path}" in
      */axnoded.inventory.json)
        fetch_axnoded_inventory "${file_path}"
        ;;
      */imagemgr.details.json)
        fetch_imagemgr_details "${file_path}"
        ;;
    esac
  done

  echo "timed out waiting for ${description}" >&2
  cat "${file_path}" >&2 || true
  return 1
}

start_container() {
  local runtime_name="$1"
  local runtime_id="$2"
  local stdout_path="$3"
  local stderr_path="$4"

  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime "${runtime_name}" \
    -runtime-id "${runtime_id}" \
    -rootfs-src image \
    -image-url "${IMAGE_URL}" \
    -stdout "${stdout_path}" \
    -stderr "${stderr_path}" \
    -shell-command "sleep 300" \
  | awk -F= '/^container_id=/{print $2}'
}

inventory_file="/tmp/axnoded.inventory.json"
details_file="/tmp/imagemgr.details.json"
runtime_name="runsc"
runtime_id="retention-${runtime_name}"
case "${AXNODED_IDLE_RUNTIME_RETENTION_TTL}" in
  *ms)
    ttl_ms="${AXNODED_IDLE_RUNTIME_RETENTION_TTL%ms}"
    ttl_seconds=$(( (ttl_ms + 999) / 1000 ))
    ;;
  *s)
    ttl_seconds="${AXNODED_IDLE_RUNTIME_RETENTION_TTL%s}"
    ;;
  *m)
    ttl_minutes="${AXNODED_IDLE_RUNTIME_RETENTION_TTL%m}"
    ttl_seconds=$(( ttl_minutes * 60 ))
    ;;
  *h)
    ttl_hours="${AXNODED_IDLE_RUNTIME_RETENTION_TTL%h}"
    ttl_seconds=$(( ttl_hours * 3600 ))
    ;;
  *)
    echo "unsupported retention ttl for test: ${AXNODED_IDLE_RUNTIME_RETENTION_TTL}" >&2
    exit 1
    ;;
esac
if [ "${ttl_seconds}" -lt 1 ]; then
  ttl_seconds=1
fi

fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "initial empty inventory snapshot" \
  "${inventory_file}" \
  20 \
  '.version == "v1alpha2" and (.heat.locality | type == "array") and .components.axnoded.running_containers == 0 and .heat.retained_runtime_count == 0 and .heat.retained_rootfs_count == 0'

container_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.retention.first.stdout" "/tmp/${runtime_name}.retention.first.stderr")"
[ -n "${container_id}" ] || {
  echo "first start did not return a container id" >&2
  exit 1
}
axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}"
container_id=""

fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "inventory retained counts after first delete" \
  "${inventory_file}" \
  30 \
  '.heat.retained_runtime_count == 1 and .heat.retained_rootfs_count == 1 and .components.imagemgr.mounted_image_count >= 1 and (.heat.mounted_image_urls | index($image_url) != null) and any(.heat.locality[]?; .key == ("image:" + $image_url) and .retained_runtime_count >= 1 and .retained_rootfs_count >= 1 and .mounted == true)' \
  --arg image_url "${IMAGE_URL}"

fetch_imagemgr_details "${details_file}"
wait_for_jq \
  "imagemgr retained mount detail after first delete" \
  "${details_file}" \
  30 \
  'any(.mounts[]?; .image_url == $image_url)' \
  --arg image_url "${IMAGE_URL}"

container_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.retention.second.stdout" "/tmp/${runtime_name}.retention.second.stderr")"
[ -n "${container_id}" ] || {
  echo "second start did not return a container id" >&2
  exit 1
}
axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}"
container_id=""

fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "inventory retained counts after second delete" \
  "${inventory_file}" \
  30 \
  '.heat.retained_runtime_count == 1 and .heat.retained_rootfs_count == 1 and any(.heat.locality[]?; .key == ("image:" + $image_url) and .retained_runtime_count >= 1 and .retained_rootfs_count >= 1)' \
  --arg image_url "${IMAGE_URL}"

metrics_output="$(fetch_metrics)"
metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"
metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retention_reuse_total" "counter" "1" \
  "axern.kind=runtime" "axern.rootfs_type=image"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retention_reuse_total" "counter" "1" \
  "axern.kind=rootfs" "axern.rootfs_type=image"

wait_for_jq \
  "inventory to drop retained runtime after ttl" \
  "${inventory_file}" \
  "$((ttl_seconds + 10))" \
  '.heat.retained_runtime_count == 0 and .heat.retained_rootfs_count == 0 and .components.imagemgr.mounted_image_count == 0 and (.heat.mounted_image_urls | index($image_url) == null) and all(.heat.locality[]?; .key != ("image:" + $image_url) or (.retained_runtime_count == 0 and .retained_rootfs_count == 0))' \
  --arg image_url "${IMAGE_URL}"

fetch_imagemgr_details "${details_file}"
wait_for_jq \
  "imagemgr to drop retained mount after ttl" \
  "${details_file}" \
  15 \
  'all(.mounts[]?; .image_url != $image_url)' \
  --arg image_url "${IMAGE_URL}"

metrics_output="$(fetch_metrics)"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retention_eviction_total" "counter" "1" \
  "axern.kind=runtime" "axern.rootfs_type=image" "axern.reason=ttl_expired"
metricsz_assert_value "${metrics_output}" "axern.axnoded_retention_eviction_total" "counter" "1" \
  "axern.kind=rootfs" "axern.rootfs_type=image" "axern.reason=ttl_expired"

container_id="$(start_container "${runtime_name}" "${runtime_id}" "/tmp/${runtime_name}.retention.third.stdout" "/tmp/${runtime_name}.retention.third.stderr")"
[ -n "${container_id}" ] || {
  echo "third start did not return a container id" >&2
  exit 1
}
axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}"
container_id=""

metrics_output="$(fetch_metrics)"
metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "2" \
  "axern.start_class=cold" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"
metricsz_assert_value "${metrics_output}" "axern.axnoded_startup_total" "counter" "1" \
  "axern.start_class=warm" "axern.runtime=${runtime_name}" "axern.rootfs_type=image" "axern.result=ok"

echo "verify_node_retention_e2e_ok=true"
