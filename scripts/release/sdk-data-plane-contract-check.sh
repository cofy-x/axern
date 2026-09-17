#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bash -n \
  "${AXERN_ROOT}/scripts/release/homebrew-formula-check.sh" \
  "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" \
  "${AXERN_ROOT}/scripts/release/local-release-smoke.sh" \
  "${AXERN_ROOT}/scripts/release/sdk-data-plane-acceptance.sh"
node --check "${AXERN_ROOT}/scripts/release/sdk-data-plane/typescript.mjs"
test -z "$(gofmt -l "${AXERN_ROOT}/scripts/release/sdk-data-plane/main.go")" || {
  echo "Go SDK data-plane acceptance fixture is not formatted" >&2
  exit 1
}

python3 - "${AXERN_ROOT}" <<'PY'
import ast
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
ast.parse((root / "scripts/release/sdk-data-plane/python.py").read_text())

workflow = (root / ".github/workflows/release.yml").read_text()
required_workflow = (
    "workflow_dispatch:",
    "needs: [artifacts, images, sdk-artifacts]",
    "local-candidate-acceptance:",
    "homebrew-formula-check:",
    "needs: [sdk-artifacts, candidate-acceptance, local-candidate-acceptance, homebrew-formula-check]",
    "needs: [artifacts, images, candidate-acceptance, local-candidate-acceptance, homebrew-formula-check, sdk-publish]",
    "local-release-smoke.sh",
    "homebrew-formula-check.sh",
    "sdk-data-plane-acceptance.sh candidate",
    "sdk-data-plane-acceptance.sh published",
)
for value in required_workflow:
    if value not in workflow:
        raise SystemExit(f"release workflow is missing SDK data-plane contract: {value}")
if workflow.count("if: github.ref_type == 'tag'") != 4:
    raise SystemExit("release workflow must restrict all publication jobs to tag events")
global_env = workflow.split("jobs:", 1)[0]
if "AXERN_RELEASE_VERSION:" in global_env:
    raise SystemExit("candidate image version must not override SDK or CLI artifact versions globally")

harness = (root / "scripts/release/kind-acceptance.sh").read_text()
for value in (
    "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES:-1073741824",
    "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES must be a positive decimal integer",
    "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES must reserve at least 1073741824 bytes for runtime conformance and axnoded headroom",
    "AXERN_RELEASE_CAPABILITY_READY_TIMEOUT_SECONDS:-300",
    "AXERN_RELEASE_CAPABILITY_READY_TIMEOUT_SECONDS must be a positive decimal integer",
    'node.memorySystemReserveBytes=${release_test_memory_system_reserve_bytes}',
    'node.enrollment.existingSecret=',
    "AXERN_SDK_ACCEPTANCE_CONFIG",
    "AXERN_SDK_ACCEPTANCE_CONTEXT=release",
    "AXERN_SDK_ACCEPTANCE_CLI",
    "AXERN_SDK_ACCEPTANCE_IMAGE_MOUNT",
    "namespace create default --output json",
    "doctor --namespace default --output json",
    "cpu: 100m",
    "memory: 512MiB",
    "PLATFORM_CAPABILITY_NETWORK_BRIDGE",
    "PLATFORM_CAPABILITY_NETWORK_BPFNET",
    "PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT",
    "admin node capability snapshot",
    '--timeout 15m run --wait-timeout 15m --file',
):
    if value not in harness:
        raise SystemExit(f"kind acceptance is missing SDK hook contract: {value}")

namespace_create = harness.index("namespace create default --output json")
doctor = harness.index("doctor --namespace default --output json")
if namespace_create > doctor:
    raise SystemExit("kind acceptance must create its namespace before running doctor")

fixtures = (
    root / "scripts/release/sdk-data-plane/python.py",
    root / "scripts/release/sdk-data-plane/typescript.mjs",
    root / "scripts/release/sdk-data-plane/main.go",
)
for fixture in fixtures:
    text = fixture.read_text()
    for value in ("run-id", "python311", "release-ok", "AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR"):
        if value not in text:
            raise SystemExit(f"{fixture.relative_to(root)} is missing acceptance behavior: {value}")
    if "100m" not in text or "512MiB" not in text:
        raise SystemExit(f"{fixture.relative_to(root)} must declare bounded release-smoke resources")

python_fixture = fixtures[0].read_text()
for value in ("DeclaredOutput", "ImageMount", "assert_read_only_image_mount", "get_sealed_output_manifest", "download_sealed_output", "sealed_output=true"):
    if value not in python_fixture:
        raise SystemExit(f"Python SDK acceptance is missing external-runner output contract: {value}")

acceptance = (root / "scripts/release/sdk-data-plane-acceptance.sh").read_text()
for value in ("run get", '${language}.run-id', "run_sdk python", "run_sdk typescript", "run_sdk go", "AXERN_SDK_ACCEPTANCE_PROCESS_TIMEOUT_SECONDS", "timeout --signal=TERM --kill-after=5s"):
    if value not in acceptance:
        raise SystemExit(f"SDK data-plane harness is missing CLI handshake contract: {value}")

local_smoke = (root / "scripts/release/local-release-smoke.sh").read_text()
for value in ('print("hello from axern")', 'print("hello from stderr"', "run_status", '"${run_status}" -ne 7', "--request-cpu 100m --request-memory 512MiB"):
    if value not in local_smoke:
        raise SystemExit(f"local release smoke is missing foreground Run behavior: {value}")

if (root / "scripts/release/verify-published-sdks.sh").exists():
    raise SystemExit("obsolete import-only published SDK verifier must not exist")
PY

if AXERN_SDK_ACCEPTANCE_CONFIG=unused \
  AXERN_SDK_ACCEPTANCE_CONTEXT=unused \
  AXERN_SDK_ACCEPTANCE_CLI=unused \
  AXERN_SDK_ACCEPTANCE_PROCESS_TIMEOUT_SECONDS=300 \
  bash "${AXERN_ROOT}/scripts/release/sdk-data-plane-acceptance.sh" candidate >/dev/null 2>&1; then
  echo "SDK data-plane acceptance accepted a process timeout without cleanup headroom" >&2
  exit 1
fi

if AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES=0 \
  bash "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" >/dev/null 2>&1; then
  echo "kind acceptance accepted an invalid test memory system reserve" >&2
  exit 1
fi

if AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES=536870912 \
  bash "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" >/dev/null 2>&1; then
  echo "kind acceptance accepted a reserve with no axnoded headroom" >&2
  exit 1
fi

if AXERN_RELEASE_CAPABILITY_READY_TIMEOUT_SECONDS=0 \
  bash "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" >/dev/null 2>&1; then
  echo "kind acceptance accepted an invalid capability readiness timeout" >&2
  exit 1
fi

echo "sdk_data_plane_contract_ok=true"
