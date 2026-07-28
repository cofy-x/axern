#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
case "${VERIFY_DOCKER_PLATFORM}" in
  linux/amd64)
    default_nydus_image="ghcr.io/dragonflyoss/image-service/nginx@sha256:e263899b73cfecb68980fbe396dbac8dbabd108397786bed8b423c496500a2a7"
    ;;
  linux/arm64 | linux/arm64/v8)
    default_nydus_image="ghcr.io/dragonflyoss/image-service/nginx@sha256:02cde82e5688297fdc6e011b4a4a5535ee106a3d39ecdef8005b45244e39ede2"
    ;;
  *)
    echo "unsupported VERIFY_DOCKER_PLATFORM for nydus e2e: ${VERIFY_DOCKER_PLATFORM}" >&2
    exit 1
    ;;
esac

export VERIFY_DOCKER_PLATFORM
export E2E_KIND="nydus"
if [ -n "${NYDUS_TEST_IMAGE:-}" ]; then
  IMAGE_URL="${NYDUS_TEST_IMAGE}"
  NYDUS_TEST_IMAGE_SOURCE="${NYDUS_TEST_IMAGE_SOURCE:-registry}"
else
  IMAGE_URL="${default_nydus_image}"
  NYDUS_TEST_IMAGE_SOURCE="${NYDUS_TEST_IMAGE_SOURCE:-local-build}"
fi
prepare_nydus_test_image_source "${IMAGE_URL}"
export IMAGE_URL="${PREPARED_NYDUS_TEST_IMAGE}"
export EXPECT_MOUNT_TYPE="nydus"
if [ "${NYDUS_TEST_IMAGE_SOURCE}" = "local-build" ]; then
  export PROBE_PATH="/bin/sh"
  export VERIFY_ARGV_JSON='["/bin/sh","-lc","echo nydus-rootfs-ok"]'
else
  export PROBE_PATH="/usr/sbin/nginx"
  # `nginx -v` is flaky under `runsc + nydus`: it can occasionally leave a
  # long-lived nginx process behind, which turns the image smoke test into a
  # wait race rather than a rootfs validation. Keep the registry fixture probe
  # focused on what this E2E needs to prove: the rootfs is runnable and nginx
  # is present.
  export VERIFY_ARGV_JSON='["/bin/sh","-lc","test -x /usr/sbin/nginx && echo nydus-rootfs-ok"]'
fi
export VERIFY_EXPECT_STDOUT="${VERIFY_EXPECT_STDOUT:-nydus-rootfs-ok}"
export VERIFY_EXPECT_STDERR="${VERIFY_EXPECT_STDERR:-}"
export VERIFY_EXPECT_EXIT="${VERIFY_EXPECT_EXIT:-0}"

bash "${ROOT_DIR}/scripts/verify/verify-node-image-e2e.sh"
