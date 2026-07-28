#!/usr/bin/env bash
set -euo pipefail

IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
AXNODED_HTTP_URL="${AXNODED_HTTP_URL:-http://127.0.0.1:23001}"
OCI_IMAGE_URL="${OCI_IMAGE_URL:?OCI_IMAGE_URL is required}"
NYDUS_IMAGE_URL="${NYDUS_IMAGE_URL:?NYDUS_IMAGE_URL is required}"
CREATE_SANDBOX_TIMEOUT="${CREATE_SANDBOX_TIMEOUT:-300s}"

oci_container_id=""
nydus_container_id=""
inventory_file="/tmp/axnoded.locality.inventory.json"

log_phase() {
  echo "locality_phase=$1"
}

dump_locality_context() {
  local phase="$1"
  local runtime_id="$2"
  local image_url="$3"
  local stdout_path="$4"
  local stderr_path="$5"
  local create_output_path="$6"

  echo "--- locality failure context ---" >&2
  echo "phase=${phase}" >&2
  echo "runtime_id=${runtime_id}" >&2
  echo "image_url=${image_url}" >&2
  echo "create_timeout=${CREATE_SANDBOX_TIMEOUT}" >&2
  echo "--- verify-cli output ---" >&2
  cat "${create_output_path}" >&2 || true
  echo "--- sandbox stdout tail (${stdout_path}) ---" >&2
  tail -n 80 "${stdout_path}" >&2 || true
  echo "--- sandbox stderr tail (${stderr_path}) ---" >&2
  tail -n 120 "${stderr_path}" >&2 || true
  echo "--- axnoded readyz ---" >&2
  curl -fsS "${AXNODED_HTTP_URL}/readyz" >&2 || true
  echo >&2
  echo "--- axnoded inventoryz ---" >&2
  curl -fsS "${AXNODED_HTTP_URL}/inventoryz" >&2 || true
  echo >&2
  echo "--- cached inventory snapshot ---" >&2
  cat "${inventory_file}" >&2 || true
  echo >&2
}

cleanup() {
  if [ -n "${oci_container_id}" ]; then
    axctl --address "${AXNODED_SOCKET}" sandbox delete "${oci_container_id}" >/dev/null 2>&1 || true
  fi
  if [ -n "${nydus_container_id}" ]; then
    axctl --address "${AXNODED_SOCKET}" sandbox delete "${nydus_container_id}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

for _ in $(seq 1 40); do
  if [ -S "${IMAGEMGR_SOCKET}" ] && [ -S "${AXNODED_SOCKET}" ] && curl -fsS "${AXNODED_HTTP_URL}/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${IMAGEMGR_SOCKET}" ] || ! [ -S "${AXNODED_SOCKET}" ] || ! curl -fsS "${AXNODED_HTTP_URL}/readyz" >/dev/null 2>&1; then
  echo "required sockets or axnoded readiness not ready" >&2
  exit 1
fi

fetch_axnoded_inventory() {
  local body_file="$1"
  local status=""
  local started_at="${SECONDS}"
  while [ $((SECONDS - started_at)) -lt 40 ]; do
    status="$(curl -sS -o "${body_file}" -w '%{http_code}' "${AXNODED_HTTP_URL}/inventoryz" || true)"
    if [ "${status}" = "200" ]; then
      return 0
    fi
    sleep 1
  done
  echo "axnoded inventory did not become ready, last status=${status}" >&2
  cat "${body_file}" >&2 || true
  return 1
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
    fetch_axnoded_inventory "${file_path}"
  done

  echo "timed out waiting for ${description}" >&2
  cat "${file_path}" >&2 || true
  return 1
}

start_container() {
  local phase="$1"
  local runtime_id="$2"
  local image_url="$3"
  local stdout_path="$4"
  local stderr_path="$5"
  local create_output_path="/tmp/${runtime_id}.create.out"

  if ! verify-cli \
    -address "${AXNODED_SOCKET}" \
    -runtime runsc \
    -runtime-id "${runtime_id}" \
    -rootfs-src image \
    -image-url "${image_url}" \
    -stdout "${stdout_path}" \
    -stderr "${stderr_path}" \
    -create-timeout "${CREATE_SANDBOX_TIMEOUT}" \
    -shell-command "sleep 300" \
    >"${create_output_path}" 2>&1; then
    dump_locality_context "${phase}" "${runtime_id}" "${image_url}" "${stdout_path}" "${stderr_path}" "${create_output_path}"
    return 1
  fi

  local container_id=""
  container_id="$(awk -F= '/^container_id=/{print $2}' "${create_output_path}")"
  if [ -z "${container_id}" ]; then
    dump_locality_context "${phase}" "${runtime_id}" "${image_url}" "${stdout_path}" "${stderr_path}" "${create_output_path}"
    return 1
  fi
  printf '%s\n' "${container_id}"
}

log_phase "initial-inventory"
fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "initial locality-ready inventory snapshot" \
  "${inventory_file}" \
  20 \
  '.version == "v1alpha2" and (.heat.locality | type == "array") and .sources.imagemgr.status == "ready" and .components.imagemgr.reachable == true'

log_phase "oci-start"
oci_runtime_id="locality-oci-$$"
oci_container_id="$(start_container "oci-start" "${oci_runtime_id}" "${OCI_IMAGE_URL}" "/tmp/locality-oci.stdout" "/tmp/locality-oci.stderr")"
[ -n "${oci_container_id}" ] || {
  echo "OCI locality start did not return a container id" >&2
  exit 1
}
axctl --address "${AXNODED_SOCKET}" sandbox delete "${oci_container_id}"
oci_container_id=""

log_phase "oci-retention-assert"
fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "OCI locality entry with retention heat" \
  "${inventory_file}" \
  40 \
  'any(.heat.locality[]?; .key == ("image:" + $image_url) and .mount_type == "oci" and .mounted == true and .retained_runtime_count >= 1 and .retained_rootfs_count >= 1)' \
  --arg image_url "${OCI_IMAGE_URL}"

log_phase "nydus-start"
nydus_runtime_id="locality-nydus-$$"
nydus_container_id="$(start_container "nydus-start" "${nydus_runtime_id}" "${NYDUS_IMAGE_URL}" "/tmp/locality-nydus.stdout" "/tmp/locality-nydus.stderr")"
[ -n "${nydus_container_id}" ] || {
  echo "Nydus locality start did not return a container id" >&2
  exit 1
}

log_phase "nydus-heat-assert"
fetch_axnoded_inventory "${inventory_file}"
wait_for_jq \
  "Nydus locality entry with daemon heat" \
  "${inventory_file}" \
  60 \
  'any(.heat.locality[]?; .key == ("image:" + $image_url) and .mount_type == "nydus" and .mounted == true and .nydus_daemon_alive == true and .chunkdb_total_chunks >= 0 and .peer_healthy_count >= 0 and .peer_hinted_count >= 0)' \
  --arg image_url "${NYDUS_IMAGE_URL}"

axctl --address "${AXNODED_SOCKET}" sandbox delete "${nydus_container_id}"
nydus_container_id=""

echo "verify_node_locality_e2e_ok=true"
