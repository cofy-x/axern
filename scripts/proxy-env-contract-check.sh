#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=proxy-env.sh
source "${AXERN_ROOT}/scripts/proxy-env.sh"
# shellcheck source=dev-env/platform.sh
source "${AXERN_ROOT}/scripts/dev-env/platform.sh"

bash -n \
  "${AXERN_ROOT}/scripts/proxy-env.sh" \
  "${AXERN_ROOT}/scripts/dev-env/platform.sh" \
  "${AXERN_ROOT}/scripts/dev-env/build-nydus-builder-image.sh" \
  "${AXERN_ROOT}/scripts/devbox/devbox.sh" \
  "${AXERN_ROOT}/scripts/release/kind-acceptance.sh" \
  "${AXERN_ROOT}/runtime/axnoded/scripts/lib/verify-docker-common.sh"

test "$(container_proxy_url 'http://127.0.0.1:18080')" = 'http://host.docker.internal:18080'
test "$(container_proxy_url 'http://localhost:8080')" = 'http://host.docker.internal:8080'
test "$(container_proxy_url 'https://proxy.example.test:8443')" = 'https://proxy.example.test:8443'
test "$(append_no_proxy_entries 'localhost,.svc' '.svc,10.0.0.0/8')" = 'localhost,.svc,10.0.0.0/8'

(
  export HTTP_PROXY='http://127.0.0.1:18080'
  export HTTPS_PROXY='http://localhost:18080'
  configure_kind_proxy >/dev/null
  test "${HTTP_PROXY}" = 'http://host.docker.internal:18080'
  test "${HTTPS_PROXY}" = 'http://host.docker.internal:18080'
  test "${http_proxy:-}" = "${HTTP_PROXY}"
  test "${https_proxy:-}" = "${HTTPS_PROXY}"
)

(
  unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy
  configure_kind_proxy >/dev/null
  test -z "${HTTP_PROXY:-}"
  test -z "${HTTPS_PROXY:-}"
)

(
  export LOCAL_REGISTRY_NAME='axern-registry'
  export LOCAL_REGISTRY_HOST='localhost:5001'
  export LOCAL_REGISTRY_CLUSTER_HOST='host.docker.internal:5001'
  export K8S_NAMESPACE='axern-system'
  export NO_PROXY='proxy.example.test,.svc'
  configure_compose_no_proxy
  test "${NO_PROXY}" = "${no_proxy:-}"
  for expected in proxy.example.test localhost host.docker.internal .svc; do
    case ",${NO_PROXY}," in
      *",${expected},"*) ;;
      *) echo "compose NO_PROXY is missing ${expected}: ${NO_PROXY}" >&2; exit 1 ;;
    esac
  done
)

(
  export REGISTRY_PROXY_URL=''
  export REGISTRY_NO_PROXY=''
  # shellcheck source=../runtime/axnoded/scripts/lib/verify-docker-common.sh
  source "${AXERN_ROOT}/runtime/axnoded/scripts/lib/verify-docker-common.sh"
  resolve_docker_daemon_proxy() { return 0; }

  export VERIFY_DOCKER_HTTP_PROXY='http://127.0.0.1:18080'
  test "$(resolve_build_http_proxy)" = 'http://host.docker.internal:18080'
  export VERIFY_DOCKER_HTTP_PROXY=''
  export HTTP_PROXY='http://localhost:18081'
  test "$(resolve_build_http_proxy)" = 'http://host.docker.internal:18081'
  export VERIFY_DOCKER_HTTPS_PROXY='https://[::1]:18443'
  test "$(resolve_build_https_proxy)" = 'https://host.docker.internal:18443'
)

python3 - "${AXERN_ROOT}" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
release_harness = root / "scripts/release/kind-acceptance.sh"
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
harness = release_harness.read_text()
for value in required:
    if value not in harness:
        raise SystemExit(f"kind acceptance is missing proxy contract: {value}")

verify_docker = (root / "runtime/axnoded/scripts/lib/verify-docker-common.sh").read_text()
for function_name in ("resolve_build_http_proxy", "resolve_build_https_proxy"):
    start = verify_docker.index(f"{function_name}() {{")
    end = verify_docker.index("\n}\n", start)
    function_body = verify_docker[start:end]
    if "normalize_runtime_proxy_url" not in function_body:
        raise SystemExit(f"{function_name} must normalize proxies for Docker build access")

policy_paths = (
    root / "scripts/dev-env/platform.sh",
    root / "scripts/dev-env/build-nydus-builder-image.sh",
    root / "scripts/devbox/devbox.sh",
    root / "runtime/axnoded/scripts/lib/verify-docker-common.sh",
)
deprecated_policy = (
    ":7890",
    "KIND_PROXY_PORT",
    "COMPOSE_PROXY_PORT",
    "LOCAL_PROXY_PORT",
    "AXERN_LOCAL_PROXY_URL",
    "AXERN_LOCAL_PROXY_AUTODETECT",
    "DEVBOX_BUILD_PROXY=auto",
)
for path in policy_paths:
    content = path.read_text()
    for value in deprecated_policy:
        if value in content:
            raise SystemExit(f"{path.relative_to(root)} contains deprecated proxy policy: {value}")
PY

rendered="$(helm template axern "${AXERN_ROOT}/deploy/helm/axern" \
  --set-string 'node.memorySystemReserveBytes=1' \
  --set-string 'proxyEnv.HTTP_PROXY=http://host.docker.internal:18080' \
  --set-string 'proxyEnv.NO_PROXY=localhost\,127.0.0.1\,.svc')"
grep -Fq 'HTTP_PROXY: "http://host.docker.internal:18080"' <<<"${rendered}"
grep -Fq 'NO_PROXY: "localhost,127.0.0.1,.svc"' <<<"${rendered}"

echo "proxy_env_contract_ok=true"
