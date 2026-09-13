#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "${SCRIPT_DIR}/../lib/verify-docker-common.sh"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
CONTAINER_NAME="${CONTAINER_NAME:-axnoded-bpfnet-example-udp-ingress}"
NAT_BACKEND="${NAT_BACKEND:-ebpf}"
RUNTIME_BINARY="${RUNTIME_BINARY:-/usr/local/bin/runsc}"
export VERIFY_DOCKER_PLATFORM CONTAINER_NAME NAT_BACKEND RUNTIME_BINARY

ensure_verify_image
run_verify_container \
  /bin/bash /workspace/scripts/demo/example-bpfnet-udp-ingress-in-container.sh
