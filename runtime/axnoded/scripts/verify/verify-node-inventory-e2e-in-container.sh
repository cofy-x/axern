#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
IMAGE_URL="${IMAGE_URL:?IMAGE_URL is required}"
AXNODED_CONTAINER_ROOT="${AXNODED_CONTAINER_ROOT:-/var/lib/axnoded/root/containers}"
# shellcheck source-path=SCRIPTDIR/..
source "${SCRIPT_DIR}/../lib/metricsz.sh"
REQUEST_ONLY_STATUS_FILTER='
  .ResourceSpec.requests.cpu_milli == 250 and
  .ResourceSpec.requests.memory_bytes == 134217728 and
  (.ResourceSpec.limits == null or (.ResourceSpec.limits.cpu_milli == 0 and .ResourceSpec.limits.memory_bytes == 0)) and
  .LinuxResources.cpu_shares > 0 and
  (.LinuxResources.memory_limit_in_bytes == null or .LinuxResources.memory_limit_in_bytes == 0)
'

container_id=""
mounted_image=""

cleanup() {
  if [ -n "${container_id}" ]; then
    axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}" >/dev/null 2>&1 || true
  fi
  if [ -n "${mounted_image}" ]; then
    payload="$(jq -cn --arg image_url "${mounted_image}" '{image_url:$image_url,lease_id:"node-inventory-e2e",owner:"verification"}')"
    curl -sS --unix-socket "${IMAGEMGR_SOCKET}" \
      -H 'Content-Type: application/json' \
      -d "${payload}" \
      http://unix/oci_umount >/dev/null 2>&1 || true
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
  local status=""
  local started_at="${SECONDS}"
  while [ $((SECONDS - started_at)) -lt 40 ]; do
    status="$(curl -sS -o "${body_file}" -w '%{http_code}' "http://127.0.0.1:23001/inventoryz" || true)"
    if [ "${status}" = "200" ]; then
      return 0
    fi
    sleep 1
  done
  echo "axnoded inventory did not become ready, last status=${status}" >&2
  cat "${body_file}" >&2 || true
  return 1
}

fetch_imagemgr_inventory() {
  local body_file="$1"
  curl -fsS --unix-socket "${IMAGEMGR_SOCKET}" http://unix/inventory >"${body_file}"
}

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
    sleep 2
    if [ "${file_path}" = "/tmp/axnoded.inventory.json" ]; then
      fetch_axnoded_inventory "${file_path}"
    else
      fetch_imagemgr_inventory "${file_path}"
    fi
  done

  echo "timed out waiting for ${description}" >&2
  cat "${file_path}" >&2 || true
  return 1
}

wait_for_status_file() {
  local container_id="$1"
  local max_wait="$2"
  local status_file="${AXNODED_CONTAINER_ROOT}/${container_id}/status"
  local started_at="${SECONDS}"

  while [ $((SECONDS - started_at)) -lt "${max_wait}" ]; do
    if [ -f "${status_file}" ]; then
      printf '%s\n' "${status_file}"
      return 0
    fi
    sleep 1
  done

  echo "request-only container status file not found: ${status_file}" >&2
  echo "--- container root entries ---" >&2
  find "${AXNODED_CONTAINER_ROOT}" -maxdepth 2 -type f -name status -print >&2 2>/dev/null || true
  return 1
}

axnoded_inventory="/tmp/axnoded.inventory.json"
imagemgr_inventory="/tmp/imagemgr.inventory.json"
runtime_id="verify-inventory-runsc-$$"

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory initial ready snapshot" \
  "${axnoded_inventory}" \
  20 \
  '.version == "v1alpha2" and .node.name != "" and (.heat.locality | type == "array") and .sources.axnoded.status == "ready" and .components.axnoded.ready == true and .components.imagemgr.reachable == true and .components.axnoded.running_containers == 0 and .pools.cgroup.capacity >= 0 and .pools.interface.capacity >= 0'

fetch_imagemgr_inventory "${imagemgr_inventory}"
jq -e 'has("mounted_images") and (.mounted_images | type == "array")' "${imagemgr_inventory}" >/dev/null
jq -e 'has("daemons") and (.daemons | type == "array")' "${imagemgr_inventory}" >/dev/null

metricsz_wait_platform_capability_available "PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT"

container_id="$(
  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime runsc \
    -runtime-id "${runtime_id}" \
    -request-cpu-milli 250 \
    -request-memory-mib 128 \
    -limit-cpu-milli 500 \
    -limit-memory-mib 256 \
    -stdout /tmp/verify-inventory-runsc.stdout \
    -stderr /tmp/verify-inventory-runsc.stderr \
    -shell-command "sleep 300" \
  | awk -F= '/^container_id=/{print $2}'
)"
[ -n "${container_id}" ] || {
  echo "verify-cli did not return a runsc container id" >&2
  exit 1
}

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory to report a running container" \
  "${axnoded_inventory}" \
  30 \
  '.components.axnoded.running_containers >= 1 and .resources.cpu.axnoded_committed_milli > 0 and .resources.memory.axnoded_committed_bytes > 0 and .resources.cpu.axnoded_unbounded_count == 0 and .resources.memory.axnoded_unbounded_count == 0 and .resources.memory.axnoded_used_bytes >= 0 and (.sources.axnoded.status == "ready" or .sources.axnoded.status == "warming" or .sources.axnoded.status == "degraded")'

axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}"
container_id=""

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory to report no running containers" \
  "${axnoded_inventory}" \
  30 \
  '.components.axnoded.running_containers == 0'

request_only_id="verify-inventory-request-only-$$"
container_id="$(
  verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime runsc \
    -runtime-id "${request_only_id}" \
    -request-cpu-milli 250 \
    -request-memory-mib 128 \
    -stdout /tmp/verify-inventory-request-only.stdout \
    -stderr /tmp/verify-inventory-request-only.stderr \
    -shell-command "sleep 300" \
  | awk -F= '/^container_id=/{print $2}'
)"
[ -n "${container_id}" ] || {
  echo "verify-cli did not return a request-only container id" >&2
  exit 1
}

status_file="$(wait_for_status_file "${container_id}" 20)"
if ! jq -e "${REQUEST_ONLY_STATUS_FILTER}" "${status_file}" >/dev/null; then
  echo "request-only container status did not match expected resources: ${status_file}" >&2
  cat "${status_file}" >&2 || true
  echo >&2
  exit 1
fi

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory to account request-only committed resources" \
  "${axnoded_inventory}" \
  30 \
  '.components.axnoded.running_containers >= 1 and .resources.cpu.axnoded_committed_milli >= 250 and .resources.memory.axnoded_committed_bytes >= 134217728 and .resources.memory.axnoded_unbounded_count == 0'

axctl --address "${AXNODED_SOCKET}" sandbox delete "${container_id}"
container_id=""

payload="$(jq -cn --arg image_url "${IMAGE_URL}" '{image_url:$image_url,lease_id:"node-inventory-e2e",owner:"verification"}')"
mount_body="$(mktemp)"
status="$(post_imagemgr /oci_mount "${payload}" "${mount_body}")"
[ "${status}" = "200" ] || { cat "${mount_body}" >&2; exit 1; }
mount_path="$(jq -r '.mount_path' "${mount_body}")"
[ -n "${mount_path}" ] || { echo "empty mount_path returned from /oci_mount" >&2; exit 1; }
[ -d "${mount_path}" ] || { echo "mount_path is not a directory: ${mount_path}" >&2; exit 1; }
mounted_image="${IMAGE_URL}"

fetch_imagemgr_inventory "${imagemgr_inventory}"
wait_for_jq \
  "imagemgr inventory to report mounted image" \
  "${imagemgr_inventory}" \
  60 \
  'any(.mounted_images[]?; .image_url == $image_url)' \
  --arg image_url "${IMAGE_URL}"

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory to report mounted image heat" \
  "${axnoded_inventory}" \
  60 \
  '.components.imagemgr.mounted_image_count >= 1 and (.heat.mounted_image_urls | index($image_url) != null) and any(.heat.locality[]?; .key == ("image:" + $image_url) and .mounted == true and .mount_type == "oci")' \
  --arg image_url "${IMAGE_URL}"

umount_body="$(mktemp)"
status="$(post_imagemgr /oci_umount "${payload}" "${umount_body}")"
[ "${status}" = "200" ] || { cat "${umount_body}" >&2; exit 1; }
mounted_image=""

fetch_imagemgr_inventory "${imagemgr_inventory}"
wait_for_jq \
  "imagemgr inventory to drop mounted image" \
  "${imagemgr_inventory}" \
  60 \
  'all(.mounted_images[]?; .image_url != $image_url)' \
  --arg image_url "${IMAGE_URL}"

fetch_axnoded_inventory "${axnoded_inventory}"
wait_for_jq \
  "axnoded inventory to drop mounted image heat" \
  "${axnoded_inventory}" \
  60 \
  '.components.imagemgr.mounted_image_count == 0 and (.heat.mounted_image_urls | index($image_url) == null)' \
  --arg image_url "${IMAGE_URL}"

echo "verify_node_inventory_e2e_ok=true"
