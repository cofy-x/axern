#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../proxy-env.sh
source "${AXERN_ROOT}/scripts/proxy-env.sh"

bash -n \
  "${AXERN_ROOT}/scripts/proxy-env.sh" \
  "${AXERN_ROOT}/scripts/dev-env/platform.sh" \
  "${AXERN_ROOT}/scripts/release/kind-acceptance.sh"

test "$(container_proxy_url 'http://127.0.0.1:7890')" = 'http://host.docker.internal:7890'
test "$(container_proxy_url 'http://localhost:8080')" = 'http://host.docker.internal:8080'
test "$(container_proxy_url 'https://proxy.example.com:8443')" = 'https://proxy.example.com:8443'
test "$(append_no_proxy_entries 'localhost,.svc' '.svc,10.0.0.0/8')" = 'localhost,.svc,10.0.0.0/8'

python3 - "${AXERN_ROOT}" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
harness = (root / "scripts/release/kind-acceptance.sh").read_text()
required = (
    'source "${AXERN_ROOT}/scripts/proxy-env.sh"',
    "release_kind_proxy_configured=true",
    'export HTTP_PROXY="${release_http_proxy}"',
    'export HTTPS_PROXY="${release_https_proxy}"',
    'proxyEnv.HTTP_PROXY=',
    'proxyEnv.HTTPS_PROXY=',
    'proxyEnv.NO_PROXY=',
    'proxyEnv.http_proxy=',
    'proxyEnv.https_proxy=',
    'proxyEnv.no_proxy=',
    'proxyEnv.REGISTRY_PROXY_URL=',
    'proxyEnv.REGISTRY_NO_PROXY=',
)
for value in required:
    if value not in harness:
        raise SystemExit(f"kind acceptance is missing proxy contract: {value}")
if ":7890" in harness:
    raise SystemExit("kind acceptance must not hard-code a host proxy port")
PY

rendered="$(helm template axern "${AXERN_ROOT}/deploy/helm/axern" \
  --set-string 'proxyEnv.HTTP_PROXY=http://host.docker.internal:7890' \
  --set-string 'proxyEnv.NO_PROXY=localhost\,127.0.0.1\,.svc')"
grep -Fq 'HTTP_PROXY: "http://host.docker.internal:7890"' <<<"${rendered}"
grep -Fq 'NO_PROXY: "localhost,127.0.0.1,.svc"' <<<"${rendered}"

echo "release_kind_acceptance_contract_ok=true"
