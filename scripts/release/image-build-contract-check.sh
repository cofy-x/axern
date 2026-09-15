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

for workflow_path in (
    root / ".github/workflows/ci.yml",
    root / ".github/workflows/post-merge-full.yml",
):
    if 'APT_MIRROR_SOURCE: "ustc"' not in workflow_path.read_text():
        raise SystemExit(f"heavyweight GitHub verification does not select the CI APT mirror: {workflow_path}")

runtime_source = (root / "runtime/axnoded/scripts/lib/verify-docker-common.sh").read_text()
function_start = runtime_source.index("build_node_runtime_base_image()")
function_end = runtime_source.index("\n}\n", function_start)
runtime_build = runtime_source[function_start:function_end]
if "axern_docker_build" not in runtime_build:
    raise SystemExit("node runtime base must use the shared Docker build cache helper")
if "docker buildx build" in runtime_build:
    raise SystemExit("node runtime base must not bypass the shared Docker build cache helper")

function_start = runtime_source.index("import_oci_image_to_node()")
function_end = runtime_source.index("\n}\n", function_start)
image_import = runtime_source[function_start:function_end]
for contract in (
    'Content-Type: application/x-tar',
    "--data-binary @-",
    'http://unix/oci_import?ref=${encoded_ref}',
):
    if contract not in image_import:
        raise SystemExit(f"verify OCI image import is missing the streaming API contract: {contract}")
for obsolete in ("archive_path", "docker cp", "Content-Type: application/json"):
    if obsolete in image_import:
        raise SystemExit(f"verify OCI image import retains the obsolete file API contract: {obsolete}")

node_runtime_dockerfile = (root / "deploy/images/lib/node-runtime-base.Dockerfile").read_text()
for contract in (
    "go build -o /out/dns-probe ./cmd/dns-probe",
    "COPY --from=axnoded-builder /out/dns-probe /usr/local/libexec/axnoded/dns-probe",
):
    if contract not in node_runtime_dockerfile:
        raise SystemExit(f"node runtime base is missing a required build contract: {contract}")

def local_replacements(go_mod_paths):
    replacements = set()
    for go_mod_path in go_mod_paths:
        for line in (root / go_mod_path).read_text().splitlines():
            fields = line.split()
            if len(fields) == 4 and fields[0] == "replace" and fields[2] == "=>" and fields[3].startswith("../../"):
                replacements.add(fields[3].removeprefix("../../"))
    return replacements

def docker_stage(dockerfile_path, stage_name):
    text = dockerfile_path.read_text()
    marker = f" AS {stage_name}"
    start = text.index(marker)
    start = text.rfind("\nFROM ", 0, start) + 1
    end = text.find("\nFROM ", start + 1)
    return text[start:] if end < 0 else text[start:end]

stage_contracts = (
    (
        root / "deploy/images/lib/node-runtime-base.Dockerfile",
        "axnoded-builder",
        ("runtime/axnoded/go.mod", "runtime/egressd/go.mod"),
    ),
    (root / "runtime/axnoded/docker/verify/Dockerfile", "axnoded-verify-builder", ("runtime/axnoded/go.mod",)),
    (root / "runtime/axnoded/docker/benchmark/Dockerfile", "axnoded-builder", ("runtime/axnoded/go.mod",)),
    (
        root / "runtime/axnoded/docker/verify/Dockerfile",
        "controld-builder",
        ("control/controld/go.mod", "gateway/gatewayd/go.mod"),
    ),
)
for dockerfile_path, stage_name, go_mod_paths in stage_contracts:
    dockerfile = docker_stage(dockerfile_path, stage_name)
    for relative_path in local_replacements(go_mod_paths):
        if f"COPY {relative_path}/go.mod" not in dockerfile:
            raise SystemExit(
                f"{dockerfile_path.relative_to(root)} stage {stage_name} does not copy local module {relative_path}"
            )
        if f"./{relative_path}" not in dockerfile:
            raise SystemExit(
                f"{dockerfile_path.relative_to(root)} stage {stage_name} omits local module {relative_path} from go.work"
            )

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
