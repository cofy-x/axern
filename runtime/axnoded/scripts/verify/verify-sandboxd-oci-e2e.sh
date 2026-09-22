#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/../lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM

ensure_verify_image

run_verify_container \
  /bin/bash -lc \
  'set -e; runtime_binary="$(command -v runsc)"; /usr/local/bin/axnoded base-spec /tmp/node-base.json; /usr/local/bin/verify-sandboxd-oci -base-spec /tmp/node-base.json -runtime-binary "${runtime_binary}" -rootfs /opt/sample-rootfs -sandboxd-binary /usr/local/libexec/axnoded/axern-sandboxd; /usr/local/bin/axnoded base-spec /tmp/node-base.json; /usr/local/bin/verify-sandboxd-oci -base-spec /tmp/node-base.json -use-template -runtime-binary "${runtime_binary}" -rootfs /opt/sample-rootfs -sandboxd-binary /usr/local/libexec/axnoded/axern-sandboxd'

echo "verify_sandboxd_oci_e2e_ok=true"
