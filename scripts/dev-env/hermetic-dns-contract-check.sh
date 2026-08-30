#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

bash -n \
  "${AXERN_ROOT}/scripts/dev-env/platform.sh" \
  "${AXERN_ROOT}/scripts/dev-env/compose-up.sh" \
  "${AXERN_ROOT}/scripts/dev-env/compose-dns-doctor-smoke.sh"

go -C "${AXERN_ROOT}/runtime/axnoded" test ./cmd/dns-fixture

python3 - "${AXERN_ROOT}" <<'PY'
import pathlib
import subprocess
import sys

root = pathlib.Path(sys.argv[1])

deprecated_name = "AXERN_VERIFY_" + "DNS_NAMESERVERS"
tracked = subprocess.run(
    ["git", "-C", str(root), "ls-files", "-z"],
    check=True,
    stdout=subprocess.PIPE,
).stdout.split(b"\0")
for encoded in tracked:
    if not encoded:
        continue
    path = root / encoded.decode()
    try:
        content = path.read_text()
    except UnicodeDecodeError:
        continue
    if deprecated_name in content:
        raise SystemExit(f"{path.relative_to(root)} restores deprecated host DNS verification input")

dockerfile = (root / "deploy/images/lib/node-runtime-base.Dockerfile").read_text()
for required in (
    "./cmd/dns-fixture",
    "/usr/local/libexec/axnoded/dns-fixture",
):
    if required not in dockerfile:
        raise SystemExit(f"node image is missing DNS fixture contract: {required}")

compose = (root / "deploy/local/compose/docker-compose.yml").read_text()
for required in (
    "dns-fixture:",
    'entrypoint: ["/usr/local/libexec/axnoded/dns-fixture"]',
    'command: ["-listen", "0.0.0.0:53"]',
    'test: ["CMD", "/usr/local/libexec/axnoded/dns-fixture", "-check", "127.0.0.1:53"]',
    "condition: service_healthy",
):
    if required not in compose:
        raise SystemExit(f"Compose is missing DNS fixture contract: {required}")

platform = (root / "scripts/dev-env/platform.sh").read_text()
start = platform.index("configure_compose_dns_fixture() {")
end = platform.index("\n}\n", start)
function = platform[start:end]
for required in (
    "up -d --wait --wait-timeout 30 dns-fixture",
    "ps -q dns-fixture",
    "docker inspect",
    'export AXNODED_DNS_NAMESERVERS="${normalized}"',
):
    if required not in function:
        raise SystemExit(f"fixture materialization is missing contract: {required}")
for forbidden in ("/etc/resolv.conf", "scutil", "resolvectl", deprecated_name):
    if forbidden in function:
        raise SystemExit(f"fixture materialization depends on host DNS: {forbidden}")

compose_up = (root / "scripts/dev-env/compose-up.sh").read_text()
steps = (
    "write_compose_env",
    "configure_compose_dns_fixture",
    "write_compose_env",
    "compose_project_up",
)
offset = 0
for step in steps:
    offset = compose_up.index(step, offset) + len(step)

smoke = (root / "scripts/dev-env/compose-dns-doctor-smoke.sh").read_text()
if 'query_name="fixture.axern.test."' not in smoke:
    raise SystemExit("DNS doctor smoke does not query the repository fixture")
PY

echo "hermetic_dns_contract_ok=true"
