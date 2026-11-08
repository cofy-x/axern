#!/usr/bin/env bash
set -euo pipefail

lock="${1:?usage: check-public-images.sh IMAGES_LOCK}"
temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT

# Use an empty, task-owned client configuration. A runner's registry login must
# never make an inaccessible release look usable by unauthenticated consumers.
python3 - "${lock}" >"${temporary}/references" <<'PY'
from pathlib import Path
import re
import sys

expected = {"POSTGRES_IMAGE", "REGISTRY_IMAGE", "CONTROLD_IMAGE", "TUNNELD_IMAGE", "GATEWAYD_IMAGE", "NODE_ALL_IN_ONE_IMAGE"}
rows = {}
for line in Path(sys.argv[1]).read_text().splitlines():
    key, separator, reference = line.partition("=")
    if not separator or key in rows or key not in expected or not re.fullmatch(r"[^\s@]+@sha256:[a-f0-9]{64}", reference):
        raise SystemExit("invalid coordinated image lock")
    rows[key] = reference
if set(rows) != expected:
    raise SystemExit("incomplete coordinated image lock")
print("\n".join(rows[key] for key in sorted(rows)))
PY
mkdir "${temporary}/docker"
while IFS= read -r reference; do
  if ! docker --config "${temporary}/docker" manifest inspect "${reference}" >/dev/null; then
    echo "release image is not anonymously readable: ${reference}; verify package visibility and registry transport before publication" >&2
    exit 1
  fi
done <"${temporary}/references"
echo 'public_release_images_ok=true'
