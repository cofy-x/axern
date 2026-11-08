#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "${temporary}"' EXIT
mkdir "${temporary}/bin"
cat >"${temporary}/bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[ "$1 $2" = 'buildx imagetools' ]
case "$3" in
  create) printf '%s\n' "$*" >>"${TEST_CREATES}" ;;
  inspect)
    if [ "${5:-}" = --raw ]; then
      printf '{"schemaVersion":2,"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}}]}\n'
    else
      printf 'linux/amd64\n'
      if [ "${TEST_MISSING_ARM:-0}" != 1 ]; then printf 'linux/arm64\n'; fi
    fi
    ;;
  *) exit 2 ;;
esac
SH
chmod 0755 "${temporary}/bin/docker"
export PATH="${temporary}/bin:${PATH}"
export AXERN_RELEASE_REGISTRY=registry.example.test:5443/axern
export AXERN_RELEASE_VERSION=v9.8.7
export GITHUB_SHA=0123456789abcdef0123456789abcdef01234567
export TEST_CREATES="${temporary}/creates"
bash "${root}/scripts/release/build-local-image-lock.sh" "${temporary}/images.lock" >/dev/null
python3 - "${temporary}" <<'PY'
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
lines = (root / "images.lock").read_text().splitlines()
expected = {"POSTGRES_IMAGE": "postgres", "REGISTRY_IMAGE": "registry", "CONTROLD_IMAGE": "controld",
            "TUNNELD_IMAGE": "tunneld", "GATEWAYD_IMAGE": "gatewayd", "NODE_ALL_IN_ONE_IMAGE": "node-all-in-one"}
assert lines == sorted(lines) and len(lines) == len(expected)
for line in lines:
    key, reference = line.split("=", 1)
    assert re.fullmatch("registry.example.test:5443/axern/" + expected[key] + r"@sha256:[a-f0-9]{64}", reference), line
creates = (root / "creates").read_text().splitlines()
assert len(creates) == len(expected)
for image in expected.values():
    calls = [call for call in creates if "/" + image + ":candidate-" in call]
    assert len(calls) == 1
    for arch in ("amd64", "arm64"):
        assert f"registry.example.test:5443/axern/{image}:v9.8.7-{arch}" in calls[0]
PY
cp "${temporary}/images.lock" "${temporary}/accepted.lock"
if TEST_MISSING_ARM=1 bash "${root}/scripts/release/build-local-image-lock.sh" "${temporary}/images.lock" >/dev/null 2>&1; then
  echo 'incomplete platform lock was accepted' >&2
  exit 1
fi
cmp "${temporary}/accepted.lock" "${temporary}/images.lock"
echo 'local_image_lock_test_ok=true'
