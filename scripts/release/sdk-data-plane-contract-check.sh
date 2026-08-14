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
    "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES:-536870912",
    "AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES must be a positive decimal integer",
    'node.memorySystemReserveBytes=${release_test_memory_system_reserve_bytes}',
    "AXERN_SDK_ACCEPTANCE_CONFIG",
    "AXERN_SDK_ACCEPTANCE_CONTEXT=release",
    "AXERN_SDK_ACCEPTANCE_CLI",
    "namespace create default --output json",
    "doctor --namespace default --output json",
    "cpu: 100m",
    "memory: 512MiB",
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
    for value in ("service-id", "python311", "runsc", "release-ok", "AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR"):
        if value not in text:
            raise SystemExit(f"{fixture.relative_to(root)} is missing acceptance behavior: {value}")
    if "100m" not in text or "512MiB" not in text:
        raise SystemExit(f"{fixture.relative_to(root)} must declare bounded release-smoke resources")

acceptance = (root / "scripts/release/sdk-data-plane-acceptance.sh").read_text()
for value in ("service get", '${language}.service-id', "run_sdk python", "run_sdk typescript", "run_sdk go"):
    if value not in acceptance:
        raise SystemExit(f"SDK data-plane harness is missing CLI handshake contract: {value}")

local_smoke = (root / "scripts/release/local-release-smoke.sh").read_text()
for value in ('print("hello from axern")', 'print("hello from stderr"', "run_status", '"${run_status}" -ne 7', "--request-cpu 100m --request-memory 512MiB"):
    if value not in local_smoke:
        raise SystemExit(f"local release smoke is missing foreground Run behavior: {value}")

if (root / "scripts/release/verify-published-sdks.sh").exists():
    raise SystemExit("obsolete import-only published SDK verifier must not exist")
PY

if AXERN_RELEASE_TEST_MEMORY_SYSTEM_RESERVE_BYTES=0 \
  bash "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" >/dev/null 2>&1; then
  echo "kind acceptance accepted an invalid test memory system reserve" >&2
  exit 1
fi

echo "sdk_data_plane_contract_ok=true"
