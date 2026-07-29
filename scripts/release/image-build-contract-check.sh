#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export AXERN_ROOT
export AXERN_RELEASE_REGISTRY="registry.example.test/cofy-x/axern"
export AXERN_RELEASE_VERSION="v9.8.7-amd64"
source "${AXERN_ROOT}/scripts/release/images.sh"

axern_export_release_images
expected_base="${AXERN_RELEASE_REGISTRY}/node-runtime-base:${AXERN_RELEASE_VERSION}"
if [ "${NODE_RUNTIME_BASE_IMAGE_TAG}" != "${expected_base}" ]; then
  echo "release node runtime base ${NODE_RUNTIME_BASE_IMAGE_TAG} does not match ${expected_base}" >&2
  exit 1
fi

if axern_release_images | grep -Fxq "${NODE_RUNTIME_BASE_IMAGE_TAG}"; then
  echo "internal node runtime base must not become a final release manifest" >&2
  exit 1
fi

python3 - "${AXERN_ROOT}" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
source = (root / "scripts/dev-env/build-images.sh").read_text()
build = 'build_node_runtime_base_image "${APT_MIRROR_SOURCE}" "${CARGO_REGISTRY_SOURCE}"'
push = 'push_image_after_build "${NODE_RUNTIME_BASE_IMAGE_TAG}"'
consume = '--build-arg "NODE_RUNTIME_BASE_IMAGE=${NODE_RUNTIME_BASE_IMAGE_TAG}"'

positions = {name: source.find(value) for name, value in {
    "build": build,
    "push": push,
    "consume": consume,
}.items()}
missing = [name for name, position in positions.items() if position < 0]
if missing:
    raise SystemExit(f"release node runtime base contract is missing: {', '.join(missing)}")
if not positions["build"] < positions["push"] < positions["consume"]:
    raise SystemExit("node runtime base must be built and pushed before node-all-in-one consumes it")

runtime_source = (root / "runtime/axnoded/scripts/lib/verify-docker-common.sh").read_text()
function_start = runtime_source.index("build_node_runtime_base_image()")
function_end = runtime_source.index("\n}\n", function_start)
runtime_build = runtime_source[function_start:function_end]
if "axern_docker_build" not in runtime_build:
    raise SystemExit("node runtime base must use the shared Docker build cache helper")
if "docker buildx build" in runtime_build:
    raise SystemExit("node runtime base must not bypass the shared Docker build cache helper")

cache_helper = (root / "scripts/dev-env/docker-build-cache.sh").read_text()
source_label = '--label "org.opencontainers.image.source=${AXERN_OCI_SOURCE_LABEL}"'
if source_label not in cache_helper:
    raise SystemExit("shared Docker builds must carry the public source repository label")

release_builder = (root / "scripts/release/build-and-push-images.sh").read_text()
for contract in (
    "release image builds require a native ${arch} runner",
    "docker info --format '{{.Architecture}}'",
    'mode="${2:-push}"',
    'AXERN_DOCKER_PUSH_AFTER_BUILD=0',
):
    if contract not in release_builder:
        raise SystemExit(f"release architecture build contract is missing: {contract}")

release_workflow = (root / ".github/workflows/release.yml").read_text()
qualification_workflow = (root / ".github/workflows/release-images-qualification.yml").read_text()
for workflow_name, workflow in (
    ("release", release_workflow),
    ("qualification", qualification_workflow),
):
    for contract in (
        "runs-on: ${{ matrix.runner }}",
        "runner: ubuntu-24.04",
        "runner: ubuntu-24.04-arm",
    ):
        if contract not in workflow:
            raise SystemExit(f"{workflow_name} workflow native runner contract is missing: {contract}")
    if "setup-qemu-action" in workflow:
        raise SystemExit(f"{workflow_name} workflow must not enable cross-architecture emulation")
if 'build-and-push-images.sh "${{ matrix.arch }}" build' not in qualification_workflow:
    raise SystemExit("qualification workflow must build images without publishing them")
for forbidden in ("packages: write", "docker/login-action"):
    if forbidden in qualification_workflow:
        raise SystemExit(f"qualification workflow must not gain publishing authority: {forbidden}")
PY

echo "release_image_build_contract_ok=true"
