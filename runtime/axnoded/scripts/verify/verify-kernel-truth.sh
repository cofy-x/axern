#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../lib/verify-docker-common.sh"

platform="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform)}"
case "${platform}" in
  linux/amd64) arch=amd64 ;;
  linux/arm64) arch=arm64 ;;
  *) echo "unsupported kernel truth platform: ${platform}" >&2; exit 1 ;;
esac
test_dir="$(mktemp -d)"
trap 'rm -rf -- "${test_dir}"' EXIT
cd "${ROOT_DIR}"
GOOS=linux GOARCH="${arch}" CGO_ENABLED=0 "${GO:-go}" test -c -tags=kerneltruth -o "${test_dir}/resources.test" ./internal/resources
GOOS=linux GOARCH="${arch}" CGO_ENABLED=0 "${GO:-go}" test -c -tags=kerneltruth -o "${test_dir}/image.test" ./axctl/commands/image

# Private cgroup namespace: the driver can evacuate/enable controllers only
# inside this disposable container. An anonymous disk volume avoids tmpfs and
# host-shared/overlay filesystem semantics; --rm also removes that volume.
docker run --rm --privileged --cgroupns=private --platform "${platform}" \
  --mount "type=bind,src=${test_dir},dst=/tests,readonly" \
  --mount type=volume,destination=/truth-data \
  --env TMPDIR=/truth-data --entrypoint /bin/sh "${BASE_IMAGE}" -ec '
    /tests/resources.test -test.v -test.timeout=30s -test.run="^TestKillCgroupProcesses$"
    /tests/image.test -test.v -test.timeout=30s -test.run="^TestDropMountedFilePageCacheEvictsRegularFile$"
  '
echo "kernel_truth_ok=true"
