#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
tag="${GITHUB_REF_NAME:-v${version}}"
if [ "${tag}" != "v${version}" ]; then
  echo "release tag ${tag} does not match VERSION ${version}" >&2
  exit 1
fi
if [ "${GITHUB_REF_TYPE:-}" = "tag" ] && [ "$(git -C "${AXERN_ROOT}" cat-file -t "refs/tags/${tag}")" != "tag" ]; then
  echo "release tag ${tag} must be annotated" >&2
  exit 1
fi
if [ "${GITHUB_REF_TYPE:-}" = "tag" ]; then
  go_tag="sdk/go/${tag}"
  go_tag_refs="$(git -C "${AXERN_ROOT}" ls-remote origin "refs/tags/${go_tag}" "refs/tags/${go_tag}^{}")"
  go_tag_object="$(printf '%s\n' "${go_tag_refs}" | awk '$2 == "refs/tags/'"${go_tag}"'" {print $1}')"
  go_tag_commit="$(printf '%s\n' "${go_tag_refs}" | awk '$2 == "refs/tags/'"${go_tag}"'^{}" {print $1}')"
  release_commit="$(git -C "${AXERN_ROOT}" rev-parse HEAD)"
  if [ -z "${go_tag_object}" ] || [ -z "${go_tag_commit}" ]; then
    echo "Go SDK release tag ${go_tag} must exist and be annotated" >&2
    exit 1
  fi
  if [ "${go_tag_commit}" != "${release_commit}" ]; then
    echo "Go SDK release tag ${go_tag} does not point to ${release_commit}" >&2
    exit 1
  fi
fi

python3 - "${AXERN_ROOT}" "${version}" <<'PY'
import json
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
want = sys.argv[2]

def toml_version(path):
    match = re.search(r'^version\s*=\s*"([^"]+)"\s*$', path.read_text(), re.MULTILINE)
    return match.group(1) if match else None

metadata = {
    "package.json": json.loads((root / "package.json").read_text())["version"],
    "apps/docs/package.json": json.loads((root / "apps/docs/package.json").read_text())["version"],
    "pyproject.toml": toml_version(root / "pyproject.toml"),
    "sdk/python/pyproject.toml": toml_version(root / "sdk/python/pyproject.toml"),
    "sdk/typescript/package.json": json.loads((root / "sdk/typescript/package.json").read_text())["version"],
    "runtime/imagefsd/Cargo.toml": toml_version(root / "runtime/imagefsd/Cargo.toml"),
}
typescript = json.loads((root / "sdk/typescript/package.json").read_text())
if typescript.get("private") is not None:
    raise SystemExit("sdk/typescript/package.json must not declare a private publish boundary")
if typescript.get("publishConfig", {}).get("access") != "public":
    raise SystemExit("sdk/typescript/package.json must publish as a public scoped package")
chart = {}
for line in (root / "deploy/helm/axern/Chart.yaml").read_text().splitlines():
    key, _, value = line.partition(":")
    if key in {"version", "appVersion"}:
        chart[key] = value.strip().strip('"')
metadata["deploy/helm/axern/Chart.yaml version"] = chart.get("version")
metadata["deploy/helm/axern/Chart.yaml appVersion"] = chart.get("appVersion")
for path, actual in metadata.items():
    if actual != want:
        raise SystemExit(f"{path}: version {actual!r}, want {want!r}")

expected_literals = {
    "sdk/python/src/axern_sdk/__init__.py": f'__version__ = "{want}"',
    "sdk/typescript/src/version.ts": f'AXERN_VERSION = "{want}"',
    "lib/go/observability/version.go": f'return "{want}"',
}
for relative, literal in expected_literals.items():
    if literal not in (root / relative).read_text():
        raise SystemExit(f"{relative} does not declare version {want}")

values = (root / "deploy/helm/axern/values.yaml").read_text()
for image in (
    "controld", "tunneld", "gatewayd", "node-all-in-one", "python311-runtime",
    "server-base-runtime", "coding-base-runtime", "desktop-base-runtime",
    "claude-code-bundle", "codex-bundle",
):
    expected = re.escape(f"ghcr.io/cofy-x/axern/{image}:v{want}")
    if image in {"controld", "tunneld", "gatewayd", "node-all-in-one"}:
        expected = re.escape(f"repository: ghcr.io/cofy-x/axern/{image}") + r"\s+" + re.escape(f"tag: v{want}")
    if not re.search(expected, values):
        raise SystemExit(f"deploy/helm/axern/values.yaml does not reference {image} v{want}")

tool_versions = {}
for line in (root / "runtime/axnoded/runtime-tools.sh").read_text().splitlines():
    if line and not line.startswith("#"):
        key, value = line.split("=", 1)
        tool_versions[key] = value
for key in ("AXERN_GVISOR_RELEASE", "AXERN_MC_RELEASE"):
    if not tool_versions.get(key):
        raise SystemExit(f"runtime tool version {key} is missing")
for key in (
    "AXERN_GVISOR_SHA512_AMD64", "AXERN_GVISOR_SHA512_ARM64",
    "AXERN_MC_SHA256_AMD64", "AXERN_MC_SHA256_ARM64",
):
    if not re.fullmatch(r"[0-9a-f]{128}" if "SHA512" in key else r"[0-9a-f]{64}", tool_versions.get(key, "")):
        raise SystemExit(f"runtime tool digest {key} is invalid")
for relative in (
    "deploy/images/lib/node-runtime-base.Dockerfile",
    "runtime/axnoded/docker/benchmark/Dockerfile",
    "docker/devbox/Dockerfile",
    "runtime/axnoded/scripts/cache/cache-runsc.sh",
    "runtime/axnoded/scripts/cache/cache-minio-mc.sh",
):
    if "release/latest" in (root / relative).read_text():
        raise SystemExit(f"{relative} must not download a rolling runtime tool release")
PY

if grep -Eq '^[[:space:]]*replace([[:space:]]|\()' "${AXERN_ROOT}/sdk/go/go.mod"; then
  echo "sdk/go/go.mod must not contain replace directives" >&2
  exit 1
fi

grep -Fq 'var version = "'"${version}"'"' "${AXERN_ROOT}/sdk/go/version.go" || {
  echo "sdk/go/version.go does not match VERSION ${version}" >&2
  exit 1
}
echo "release_version_ok=${version}"
