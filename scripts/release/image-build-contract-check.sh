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

node_runtime_dockerfile = (root / "deploy/images/lib/node-runtime-base.Dockerfile").read_text()
for contract in (
    "COPY lib/go/agentbundle/go.mod /workspace/lib/go/agentbundle/go.mod",
    "./lib/go/agentbundle",
):
    if contract not in node_runtime_dockerfile:
        raise SystemExit(f"node runtime base is missing the agentbundle module staging contract: {contract}")

cache_helper = (root / "scripts/dev-env/docker-build-cache.sh").read_text()
source_label = '--label "org.opencontainers.image.source=${AXERN_OCI_SOURCE_LABEL}"'
if source_label not in cache_helper:
    raise SystemExit("shared Docker builds must carry the public source repository label")
for contract in (
    "AXERN_DOCKER_CACHE_BACKEND:-none",
    "ACTIONS_RUNTIME_TOKEN is required",
    "ACTIONS_RESULTS_URL or ACTIONS_CACHE_URL is required",
    "docker_build_cache_backend=gha scope=%s",
    "type=gha,scope=${cache_scope},mode=max,timeout=20m",
):
    if contract not in cache_helper:
        raise SystemExit(f"shared Docker GHA cache contract is missing: {contract}")
for obsolete in ("AXERN_DOCKER_GHA_CACHE", "AXERN_DOCKER_REGISTRY_CACHE"):
    if obsolete in cache_helper:
        raise SystemExit(f"shared Docker cache helper retains obsolete backend selection: {obsolete}")

bundle_base = "ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea"
bundle_contracts = {
    "claude-code": root / "runtime/axnoded/docker/runtimes/claude-code-bundle/Dockerfile",
    "codex": root / "runtime/axnoded/docker/runtimes/codex-bundle/Dockerfile",
}
for agent_id, dockerfile in bundle_contracts.items():
    build_script = dockerfile.parent / "build-bundle.sh"
    if not build_script.is_file():
        raise SystemExit(f"{agent_id} bundle build/audit script is missing: {build_script}")
    dockerfile_text = dockerfile.read_text()
    if "COPY --chmod=0755 build-bundle.sh" not in dockerfile_text:
        raise SystemExit(f"{agent_id} Dockerfile does not execute its external build/audit script")
    wrapper = dockerfile.parent / ("claude" if agent_id == "claude-code" else "codex")
    wrapper_text = wrapper.read_text()
    if "uname" in wrapper_text:
        raise SystemExit(f"{agent_id} wrapper must not require task-image uname")
    if "unset LD_LIBRARY_PATH" not in wrapper_text:
        raise SystemExit(f"{agent_id} wrapper does not clear task-image LD_LIBRARY_PATH")
    text = dockerfile_text + build_script.read_text()
    for contract in (
        bundle_base,
        f'io.axern.agent-bundle.agent-id="{agent_id}"',
        'io.axern.agent-bundle.architecture="linux/${TARGETARCH}"',
        "/opt/axern/agent-bundle/manifest.json",
        "readelf -h",
        "--list",
    ):
        if contract not in text:
            raise SystemExit(f"{agent_id} self-contained bundle contract is missing: {contract}")

claude_dockerfile = bundle_contracts["claude-code"].read_text()
claude_build = (bundle_contracts["claude-code"].parent / "build-bundle.sh").read_text()
claude_wrapper = (bundle_contracts["claude-code"].parent / "claude").read_text()
for contract in (
    'io.axern.agent-bundle.mount-target="/__claude_code"',
    'io.axern.agent-bundle.public-mount-target="/opt/axern/agents/claude-code"',
    "canonical_root=/__claude_code",
    'loader_wrapped_elfs: []',
    'exec "$real_binary" "$@"',
):
    if contract not in claude_dockerfile + claude_build + claude_wrapper:
        raise SystemExit(f"Claude Code dual-path bundle contract is missing: {contract}")
if '--library-path' in claude_wrapper:
    raise SystemExit("Claude Code wrapper must execute its native ELF without a loader wrapper")

codex_bundle = bundle_contracts["codex"].read_text() + (bundle_contracts["codex"].parent / "build-bundle.sh").read_text()
for contract in (
    'io.axern.agent-bundle.mount-target="/opt/axern/agents/codex"',
    "--library-path",
):
    if contract not in codex_bundle:
        raise SystemExit(f"Codex bundle contract is missing: {contract}")
for checksum in (
    "e798599612f4bb71333a3397ab0d095fd62214e115aea45aa858a145fc72d67e",
    "aa881151bd0f9f154a0424dd60a72e9ce10672619121658c278a24327ef46831",
):
    if checksum not in codex_bundle:
        raise SystemExit(f"Codex bundle is missing pinned Node checksum {checksum}")

for build_script in (
    root / "runtime/axnoded/scripts/runtime/build-claude-code-bundle-image.sh",
    root / "runtime/axnoded/scripts/runtime/build-codex-bundle-image.sh",
):
    if "verify-agent-bundle-image.sh" not in build_script.read_text():
        raise SystemExit(f"bundle build does not run compatibility verification: {build_script}")

release_builder = (root / "scripts/release/build-and-push-images.sh").read_text()
for contract in (
    "release image builds require a native ${arch} runner",
    "docker info --format '{{.Architecture}}'",
    'mode="${2:-push}"',
    'AXERN_DOCKER_PUSH_AFTER_BUILD=0',
    'AXERN_NODE_RUNTIME_BASE_CACHE_BACKEND=gha',
):
    if contract not in release_builder:
        raise SystemExit(f"release architecture build contract is missing: {contract}")

release_workflow = (root / ".github/workflows/release.yml").read_text()
for contract in (
    "workflow_dispatch:",
    "AXERN_RELEASE_VERSION: ${{ github.ref_type == 'tag' && github.ref_name || format('candidate-{0}', github.sha) }}",
    "runs-on: ${{ matrix.runner }}",
    "runner: ubuntu-24.04",
    "runner: ubuntu-24.04-arm",
    "docker/setup-buildx-action@4d04d5d9486b7bd6fa91e7baf45bbb4f8b9deedd # v4.0.0",
    "crazy-max/ghaction-github-runtime@04d248b84655b509d8c44dc1d6f990c879747487 # v4.0.0",
    "packages: write",
    "docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0",
    'build-and-push-images.sh "${{ matrix.arch }}"',
    "image-lock:",
    "build-local-image-lock.sh dist/local-image-lock/images.lock",
    "AXERN_LOCAL_IMAGE_LOCK_FILE:",
    "name: local-image-lock",
):
    if contract not in release_workflow:
        raise SystemExit(f"release candidate image contract is missing: {contract}")
if "setup-qemu-action" in release_workflow:
    raise SystemExit("release workflow must not enable cross-architecture emulation")
global_env = release_workflow.split("jobs:", 1)[0]
if "AXERN_RELEASE_VERSION:" in global_env:
    raise SystemExit("candidate image version must not override artifact versions globally")

lock_builder = (root / "scripts/release/build-local-image-lock.sh").read_text()
for contract in (
    "linux/amd64",
    "linux/arm64",
    "imagetools create",
    "imagetools inspect",
    "sha256sum",
    "POSTGRES_IMAGE",
    "OTEL_LGTM_IMAGE",
):
    if contract not in lock_builder:
        raise SystemExit(f"local image digest lock contract is missing: {contract}")

cli_builder = (root / "scripts/release/build-cli.sh").read_text()
for contract in ("AXERN_LOCAL_IMAGE_LOCK_FILE", "localbundle.imageLock", "images.lock"):
    if contract not in cli_builder:
        raise SystemExit(f"CLI release image lock injection is missing: {contract}")
PY

echo "release_image_build_contract_ok=true"
