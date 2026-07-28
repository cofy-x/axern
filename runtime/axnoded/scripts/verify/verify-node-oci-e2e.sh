#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${ROOT_DIR}/scripts/lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
case "${VERIFY_DOCKER_PLATFORM}" in
  linux/amd64)
    default_oci_image="docker.io/library/busybox@sha256:b8d1827e38a1d49cd17217efd7b07d689e4ea1744e39c7dcbb95533d175bea65"
    ;;
  linux/arm64 | linux/arm64/v8)
    default_oci_image="docker.io/library/busybox@sha256:c4e5b27bf840ba1ebd5568b6b914f6926f3559b2ad4f505b1f37aae483b907d6"
    ;;
  *)
    echo "unsupported VERIFY_DOCKER_PLATFORM for oci e2e: ${VERIFY_DOCKER_PLATFORM}" >&2
    exit 1
    ;;
esac

export VERIFY_DOCKER_PLATFORM

export E2E_KIND="oci"
export IMAGE_URL="${OCI_TEST_IMAGE:-${default_oci_image}}"
export EXPECT_MOUNT_TYPE="oci"
export PROBE_PATH="/bin/sh"
export VERIFY_ARGV_JSON='["/bin/sh","-c","echo oci_image_ok; echo oci_image_err 1>&2; sleep 1"]'
export VERIFY_EXPECT_STDOUT="${VERIFY_EXPECT_STDOUT:-oci_image_ok}"
export VERIFY_EXPECT_STDERR="${VERIFY_EXPECT_STDERR:-oci_image_err}"
export VERIFY_EXPECT_EXIT="${VERIFY_EXPECT_EXIT:-0}"

bash "${ROOT_DIR}/scripts/verify/verify-node-image-e2e.sh"
