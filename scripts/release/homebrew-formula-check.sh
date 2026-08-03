#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${root}/VERSION")"
dist="${AXERN_RELEASE_DIST:-${root}/dist/release}"
fixture_root=""
output="${1:-}"
use_fixture=false
if [[ -z "${output}" ]]; then
  use_fixture=true
fi

cleanup() {
  if [[ -n "${fixture_root}" ]]; then
    rm -rf "${fixture_root}"
  fi
}
trap cleanup EXIT

if [[ "${use_fixture}" == true ]]; then
  fixture_root="$(mktemp -d)"
  dist="${fixture_root}/release"
  mkdir -p "${dist}"
  for entry in \
    "darwin_arm64:1111111111111111111111111111111111111111111111111111111111111111" \
    "darwin_amd64:2222222222222222222222222222222222222222222222222222222222222222" \
    "linux_arm64:3333333333333333333333333333333333333333333333333333333333333333" \
    "linux_amd64:4444444444444444444444444444444444444444444444444444444444444444"; do
    platform="${entry%%:*}"
    digest="${entry#*:}"
    printf '%s  axern_%s_%s.tar.gz\n' "${digest}" "${version}" "${platform}" >> "${dist}/checksums.txt"
  done
fi

if [[ -z "${output}" ]]; then
  output="${fixture_root}/axern.rb"
fi

if [[ ! -f "${dist}/checksums.txt" ]]; then
  echo "missing release checksums: ${dist}/checksums.txt" >&2
  exit 1
fi

AXERN_RELEASE_DIST="${dist}" bash "${root}/scripts/release/homebrew-formula.sh" "${output}"
ruby -c "${output}" >/dev/null

python3 - "${root}" "${dist}" "${output}" "${version}" <<'PY'
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
dist = pathlib.Path(sys.argv[2])
formula = pathlib.Path(sys.argv[3]).read_text()
version = sys.argv[4]

if re.search(r"@[A-Z_]+@", formula):
    raise SystemExit("generated Homebrew formula contains unresolved placeholders")

checksums = {}
for line in (dist / "checksums.txt").read_text().splitlines():
    digest, filename = line.split(maxsplit=1)
    checksums[filename] = digest

base = f"https://github.com/cofy-x/axern/releases/download/v{version}"
platforms = ("darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64")
for platform in platforms:
    filename = f"axern_{version}_{platform}.tar.gz"
    digest = checksums.get(filename)
    if digest is None:
        raise SystemExit(f"release checksums do not contain {filename}")
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise SystemExit(f"release checksum for {filename} is not SHA-256")
    for expected in (f'url "{base}/{filename}"', f'sha256 "{digest}"'):
        if expected not in formula:
            raise SystemExit(f"generated Homebrew formula is missing {expected}")

for expected in (
    "class Axern < Formula",
    f'version "{version}"',
    'license "Apache-2.0"',
    'bin.install "axern"',
    'shell_output("#{bin}/axern version")',
):
    if expected not in formula:
        raise SystemExit(f"generated Homebrew formula is missing {expected}")

if formula.count("  url ") != 4 or formula.count("  sha256 ") != 4:
    raise SystemExit("generated Homebrew formula must contain exactly four platform archives")

template = (root / "packaging/homebrew/axern.rb.tmpl").read_text()
if "@VERSION@" not in template or "@BASE@" not in template:
    raise SystemExit("Homebrew formula template lost its release placeholders")
PY

echo "homebrew_formula_check_ok=${version}"
