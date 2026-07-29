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

python3 - "${AXERN_ROOT}/scripts/dev-env/build-images.sh" <<'PY'
import pathlib
import sys

source = pathlib.Path(sys.argv[1]).read_text()
build = 'build_node_runtime_base_image "${APT_MIRROR_SOURCE}" "${CARGO_REGISTRY_SOURCE}"'
push = 'push_image_after_build "${NODE_RUNTIME_BASE_IMAGE_TAG}"'
consume = '--build-arg "NODE_RUNTIME_BASE_IMAGE=${NODE_RUNTIME_BASE_IMAGE_TAG}"'
source_label = '--label "org.opencontainers.image.source=${AXERN_OCI_SOURCE_LABEL}"'

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

runtime_helper = pathlib.Path(sys.argv[1]).parents[2] / "runtime/axnoded/scripts/lib/verify-docker-common.sh"
if source_label not in runtime_helper.read_text():
    raise SystemExit("node runtime base must carry the public source repository label")
PY

echo "release_image_build_contract_ok=true"
