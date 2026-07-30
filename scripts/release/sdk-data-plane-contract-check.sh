#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bash -n \
  "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" \
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
    "needs: [artifacts, images, sdk-artifacts]",
    "sdk-data-plane-acceptance.sh candidate",
    "sdk-data-plane-acceptance.sh published",
)
for value in required_workflow:
    if value not in workflow:
        raise SystemExit(f"release workflow is missing SDK data-plane contract: {value}")

harness = (root / "scripts/release/kind-acceptance.sh").read_text()
for value in (
    "AXERN_SDK_ACCEPTANCE_CONFIG",
    "AXERN_SDK_ACCEPTANCE_CONTEXT=release",
    "AXERN_SDK_ACCEPTANCE_CLI",
    "doctor --namespace default --output json",
):
    if value not in harness:
        raise SystemExit(f"kind acceptance is missing SDK hook contract: {value}")

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

acceptance = (root / "scripts/release/sdk-data-plane-acceptance.sh").read_text()
for value in ("service get", '${language}.service-id', "run_sdk python", "run_sdk typescript", "run_sdk go"):
    if value not in acceptance:
        raise SystemExit(f"SDK data-plane harness is missing CLI handshake contract: {value}")

if (root / "scripts/release/verify-published-sdks.sh").exists():
    raise SystemExit("obsolete import-only published SDK verifier must not exist")
PY

echo "sdk_data_plane_contract_ok=true"
