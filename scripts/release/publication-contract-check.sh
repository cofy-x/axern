#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python3 "${AXERN_ROOT}/scripts/release/sdk_publication_readiness_test.py"
python3 "${AXERN_ROOT}/scripts/release/homebrew_formula_reconcile_test.py"
python3 -m unittest discover -s "${AXERN_ROOT}/scripts/release" -p test_delivery_image_audit.py
bash "${AXERN_ROOT}/scripts/release/publication-recovery-test.sh"

python3 - "${AXERN_ROOT}" <<'PY'
import ast
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
release = (root / ".github/workflows/release.yml").read_text()
homebrew = (root / ".github/workflows/homebrew.yml").read_text()
ci = (root / ".github/workflows/ci.yml").read_text()
security = (root / ".github/workflows/security.yml").read_text()

for source in (ci, release):
    if "uses: ./.github/workflows/security.yml" not in source:
        raise SystemExit("CI and Release must call the shared dependency security audit")
validate = release.split("  validate:\n", 1)[1].split("\n  artifacts:\n", 1)[0]
if "needs: security" not in validate:
    raise SystemExit("release validation must wait for the dependency security audit")
if 'AXERN_REQUIRE_PUBLISHABLE_RELEASE: "1"\n        run: make release-check' not in validate:
    raise SystemExit("release validation must reject native preview sources")
if "make security-check" not in security or "workflow_call:" not in security or "schedule:" not in security:
    raise SystemExit("security audit must run the canonical scanner gate in CI, releases and weekly")

image_lock = release.split("\n  image-lock:\n", 1)[1].split("\n  image-audit:\n", 1)[0]
image_audit = release.split("\n  image-audit:\n", 1)[1].split("\n  candidate-acceptance:\n", 1)[0]
if "delivery_image_audit.py bind" not in image_lock or "dist/local-image-lock/binding.json" not in image_lock:
    raise SystemExit("release image lock artifact must carry its digest and run binding")
for contract in ("needs: image-lock", "name: local-image-lock", "delivery_image_audit.py audit",
                 '--source "${GITHUB_SHA}" --run-id "${GITHUB_RUN_ID}" --run-attempt "${GITHUB_RUN_ATTEMPT}"',
                 "install-image-auditors.sh", "if: always()"):
    if contract not in image_audit:
        raise SystemExit(f"release image audit is missing contract: {contract}")
for job in ("sdk-publish", "publish"):
    job_body = release.split(f"\n  {job}:\n", 1)[1]
    needs = re.search(r"(?m)^    needs: \[([^\]]+)\]$", job_body)
    if not needs or "image-audit" not in needs.group(1).split(", "):
        raise SystemExit(f"{job} must wait for the release image audit")

for relative in (
    "scripts/release/sdk_publication_readiness.py",
    "scripts/release/homebrew_formula_reconcile.py",
    "scripts/release/delivery_image_audit.py",
):
    ast.parse((root / relative).read_text())

for contract in (
    "sdk-publication-readiness:",
    "needs: [publish, sdk-artifacts]",
    "timeout-minutes: 35",
    "sdk_publication_readiness.py",
    "needs: [sdk-publication-readiness]",
    "bash scripts/release/publish-image-manifests.sh",
    "bash scripts/release/publish-helm-chart.sh",
    "bash scripts/release/publish-github-release.sh",
):
    if contract not in release:
        raise SystemExit(f"release workflow is missing publication readiness contract: {contract}")
for obsolete in ("\n  homebrew:\n", "HOMEBREW_TAP_TOKEN", "HOMEBREW_TAP_ENABLED"):
    if obsolete in release:
        raise SystemExit(f"release workflow retains coupled Homebrew publication: {obsolete.strip()}")
if "scripts/release/preflight.sh" in release or 'run: helm push "dist/release/' in release:
    raise SystemExit("release workflow retains non-resumable publication steps")

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
        raise SystemExit(f"manual CI is missing release contract verification: {contract}")

for workflow, source in (("ci", ci), ("release", release)):
    for job in re.split(r"(?m)^  [a-z][a-z0-9-]*:\n", source)[1:]:
        invocation = re.search(r"make release-(?:check|build)", job)
        if invocation and "apt-get -o Acquire::Retries=0 install --no-install-recommends --yes ripgrep" not in job[:invocation.start()]:
            raise SystemExit(f"{workflow} release contract job must install ripgrep before architecture checks")
PY

echo "publication_contract_ok=true"
