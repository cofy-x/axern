#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT
mkdir "${temporary}/bin"
cat >"${temporary}/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = --config ] && [ "$3 $4" = 'manifest inspect' ]
[ -d "$2" ] && [ -z "$(ls -A "$2")" ]
[ "${DOCKER_CONFIG:-}" != "$2" ]
printf '%s\n' "$5" >>"${TEST_PUBLIC_READS}"
[ "${TEST_DENY_PUBLIC:-0}" != 1 ]
SH
chmod 0755 "${temporary}/bin/docker"
export PATH="${temporary}/bin:${PATH}"
export DOCKER_CONFIG="${temporary}/existing-auth"
export TEST_PUBLIC_READS="${temporary}/reads"
python3 - "${temporary}/images.lock" <<'PY'
from pathlib import Path
import sys
Path(sys.argv[1]).write_text("".join(key + "=ghcr.io/example/" + key.lower() + "@sha256:" + "a" * 64 + "\n"
    for key in ("POSTGRES_IMAGE", "REGISTRY_IMAGE", "CONTROLD_IMAGE", "TUNNELD_IMAGE", "GATEWAYD_IMAGE", "NODE_ALL_IN_ONE_IMAGE")))
PY
bash "${root}/scripts/release/check-public-images.sh" "${temporary}/images.lock" >/dev/null
[ "$(wc -l <"${TEST_PUBLIC_READS}")" -eq 6 ]
if TEST_DENY_PUBLIC=1 bash "${root}/scripts/release/check-public-images.sh" "${temporary}/images.lock" >/dev/null 2>&1; then
  echo 'private image accepted' >&2
  exit 1
fi
cp "${temporary}/images.lock" "${temporary}/malformed.lock"
head -n 1 "${temporary}/images.lock" >>"${temporary}/malformed.lock"
if bash "${root}/scripts/release/check-public-images.sh" "${temporary}/malformed.lock" >/dev/null 2>&1; then
  echo 'duplicate image lock accepted' >&2
  exit 1
fi
echo 'public_image_read_test_ok=true'
