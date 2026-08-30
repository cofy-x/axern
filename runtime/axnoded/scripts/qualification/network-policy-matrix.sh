#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AXNODED_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPO_ROOT="$(cd "${AXNODED_DIR}/../.." && pwd)"
source "${AXNODED_DIR}/scripts/lib/verify-docker-common.sh"

if [ "$(uname -s)" != "Linux" ]; then
  echo "network-policy performance qualification requires a native stable Linux host; Docker Desktop results are not comparable" >&2
  exit 1
fi
if ! git -C "${REPO_ROOT}" diff --quiet || ! git -C "${REPO_ROOT}" diff --cached --quiet || [ -n "$(git -C "${REPO_ROOT}" ls-files --others --exclude-standard)" ]; then
  echo "network-policy qualification requires a clean candidate checkout" >&2
  exit 1
fi

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
export VERIFY_DOCKER_PLATFORM
ensure_verify_image

runner_image_digest="$(docker image inspect --format '{{.Id}}' "${IMAGE_TAG}")"
if ! [[ "${runner_image_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "qualification runner does not have a canonical sha256 image ID" >&2
  exit 1
fi

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
host_output="${NETWORK_POLICY_QUALIFICATION_OUTPUT_DIR:-${REPO_ROOT}/output/network-policy-qualification-${timestamp}}"
mkdir -p "${host_output}"
host_output="$(cd "${host_output}" && pwd)"

docker_args=(
  run --rm --privileged
  --platform "${VERIFY_DOCKER_PLATFORM}"
  --mount "type=bind,src=${REPO_ROOT},dst=/qualification-source,readonly"
  --mount "type=bind,src=${host_output},dst=/qualification-output"
  -e NETWORK_POLICY_QUALIFICATION_REPO_ROOT=/qualification-source
  -e NETWORK_POLICY_QUALIFICATION_OUTPUT_DIR=/qualification-output
  -e "NETWORK_POLICY_QUALIFICATION_RUNNER_IMAGE_DIGEST=${runner_image_digest}"
  -e "NETWORK_POLICY_QUALIFICATION_BUILD_DIGEST=${runner_image_digest}"
)

for variable in \
  NETWORK_POLICY_QUALIFICATION_SAMPLES \
  NETWORK_POLICY_QUALIFICATION_CONCURRENCY \
  NETWORK_POLICY_QUALIFICATION_PAYLOAD_BYTES \
  NETWORK_POLICY_QUALIFICATION_SUSTAINED_SECONDS \
  NETWORK_POLICY_QUALIFICATION_RULE_SCALE_COUNTS; do
  if [ -n "${!variable:-}" ]; then
    docker_args+=(-e "${variable}=${!variable}")
  fi
done

if [ -n "${NETWORK_POLICY_QUALIFICATION_BASELINE:-}" ]; then
  baseline="$(cd "$(dirname "${NETWORK_POLICY_QUALIFICATION_BASELINE}")" && pwd)/$(basename "${NETWORK_POLICY_QUALIFICATION_BASELINE}")"
  if [ ! -f "${baseline}" ]; then
    echo "qualification baseline does not exist: ${baseline}" >&2
    exit 1
  fi
  docker_args+=(
    --mount "type=bind,src=${baseline},dst=/qualification-baseline.json,readonly"
    -e NETWORK_POLICY_QUALIFICATION_BASELINE=/qualification-baseline.json
  )
fi

docker "${docker_args[@]}" "${runner_image_digest}" \
  /bin/bash /workspace/qualification/scripts/qualification-matrix.sh

echo "network_policy_qualification_output=${host_output}"
