#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python3 "${AXERN_ROOT}/scripts/release/sdk_publication_readiness_test.py"
python3 "${AXERN_ROOT}/scripts/release/homebrew_formula_reconcile_test.py"

python3 - "${AXERN_ROOT}" <<'PY'
import ast
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
release = (root / ".github/workflows/release.yml").read_text()
homebrew = (root / ".github/workflows/homebrew.yml").read_text()
ci = (root / ".github/workflows/ci.yml").read_text()

for relative in (
    "scripts/release/sdk_publication_readiness.py",
    "scripts/release/homebrew_formula_reconcile.py",
):
    ast.parse((root / relative).read_text())

for contract in (
    "sdk-publication-readiness:",
    "needs: [publish, sdk-artifacts]",
    "timeout-minutes: 35",
    "sdk_publication_readiness.py",
    "needs: [sdk-publication-readiness]",
):
    if contract not in release:
        raise SystemExit(f"release workflow is missing publication readiness contract: {contract}")
for obsolete in ("\n  homebrew:\n", "HOMEBREW_TAP_TOKEN", "HOMEBREW_TAP_ENABLED"):
    if obsolete in release:
        raise SystemExit(f"release workflow retains coupled Homebrew publication: {obsolete.strip()}")

for contract in (
    "workflow_run:",
    "workflow_dispatch:",
    "workflows: [Release]",
    "manual Homebrew publication must run from",
    "environment: homebrew-release",
    "repository: cofy-x/homebrew-tap",
    "homebrew_formula_reconcile.py",
    "brew install cofy-x/tap/axern",
    "brew test cofy-x/tap/axern",
):
    if contract not in homebrew:
        raise SystemExit(f"Homebrew workflow is missing publication contract: {contract}")

prepare = homebrew.split("  prepare:\n", 1)[1].split("\n  publish:\n", 1)[0]
if "secrets." in prepare or "HOMEBREW_TAP_TOKEN" in prepare:
    raise SystemExit("Homebrew formula preparation must not receive cross-repository credentials")
publish = homebrew.split("\n  publish:\n", 1)[1].split("\n  verify:\n", 1)[0]
if "environment: homebrew-release" not in publish or "HOMEBREW_TAP_TOKEN" not in publish:
    raise SystemExit("Homebrew publication must use the protected tap credential")

preflight = (root / "scripts/release/sdk-preflight.sh").read_text()
if "proxy.golang.org" in preflight or "sum.golang.org" in preflight:
    raise SystemExit("pre-tag SDK preflight must not prime a negative Go module cache entry")

for contract in ("name: Release Contracts", "actionlint .github/workflows/*.yml", "make release-check"):
    if contract not in ci:
        raise SystemExit(f"pull request CI is missing release contract gate: {contract}")
PY

echo "publication_contract_ok=true"
