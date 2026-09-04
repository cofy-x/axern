#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
AXNODED_DIR="${ROOT_DIR}/runtime/axnoded"
SANDBOXD_LIBEXEC_PATH="/usr/local/libexec/axnoded/axern-sandboxd"
LOOP_DEVICE_HELPER_PATH="/usr/local/bin/axern-ensure-loop-devices"

cd "${AXNODED_DIR}"

make release-binary

for binary in axnoded axnoded-runtime-runner axern-sandboxd; do
  path="${AXNODED_DIR}/output/${binary}"
  if [[ ! -x "${path}" ]]; then
    echo "sandboxd_packaging_failed=true reason=missing_or_not_executable binary=${binary} path=${path}" >&2
    exit 1
  fi
done

for dockerfile in \
  "${ROOT_DIR}/deploy/images/lib/node-runtime-base.Dockerfile" \
  "${AXNODED_DIR}/docker/verify/Dockerfile"; do
  if ! grep -Fq "${SANDBOXD_LIBEXEC_PATH}" "${dockerfile}"; then
    echo "sandboxd_packaging_failed=true reason=missing_libexec_copy dockerfile=${dockerfile} path=${SANDBOXD_LIBEXEC_PATH}" >&2
    exit 1
  fi
done

if ! grep -Fq "COPY deploy/images/lib/ensure-loop-devices.sh ${LOOP_DEVICE_HELPER_PATH}" \
  "${ROOT_DIR}/deploy/images/lib/node-runtime-base.Dockerfile"; then
  echo "sandboxd_packaging_failed=true reason=missing_loop_device_helper path=${LOOP_DEVICE_HELPER_PATH}" >&2
  exit 1
fi
for entrypoint in \
  "${ROOT_DIR}/deploy/images/lib/node-all-in-one-entrypoint.sh" \
  "${AXNODED_DIR}/scripts/verify/node-all-in-one-entrypoint.sh"; do
  if ! grep -Fq "${LOOP_DEVICE_HELPER_PATH}" "${entrypoint}"; then
    echo "sandboxd_packaging_failed=true reason=loop_device_helper_not_invoked entrypoint=${entrypoint}" >&2
    exit 1
  fi
done

echo "sandboxd_packaging_ok=true"
