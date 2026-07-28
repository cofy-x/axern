#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
source "${REPO_ROOT}/scripts/dev-env/docker-build-cache.sh"
IMAGE_TAG="${IMAGE_TAG:-axnoded-verify:latest}"
CONTAINER_NAME="${CONTAINER_NAME:-axnoded-verify}"
VERIFY_DOCKER_VARIANT="${VERIFY_DOCKER_VARIANT:-full}"
APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-archive}"
CARGO_REGISTRY_SOURCE="${CARGO_REGISTRY_SOURCE:-crates-io}"
GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
GOSUMDB="${GOSUMDB:-sum.golang.org}"
BASE_IMAGE="${BASE_IMAGE:-ubuntu:24.04}"
GO_IMAGE="${GO_IMAGE:-golang:1.25.12}"
NGINX_IMAGE="${NGINX_IMAGE:-nginx:1.15}"
RUST_IMAGE="${RUST_IMAGE:-rust:1.89.0}"
NODE_RUNTIME_BASE_IMAGE_TAG="${NODE_RUNTIME_BASE_IMAGE_TAG:-axern/local-node-runtime-base:dev}"
VERIFY_DOCKER_BUILDKIT="${VERIFY_DOCKER_BUILDKIT:-1}"
RUNSC_SOURCE="${RUNSC_SOURCE:-auto}"
RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH:-}"
MC_SOURCE="${MC_SOURCE:-auto}"
MC_CACHE_ARCH="${MC_CACHE_ARCH:-}"

VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-}"
PERF_KERNEL_RELEASE="${PERF_KERNEL_RELEASE:-}"
VERIFY_DOCKER_HTTP_PROXY="${VERIFY_DOCKER_HTTP_PROXY:-}"
VERIFY_DOCKER_HTTPS_PROXY="${VERIFY_DOCKER_HTTPS_PROXY:-}"
VERIFY_DOCKER_NO_PROXY="${VERIFY_DOCKER_NO_PROXY:-}"
AXERN_LOCAL_PROXY_URL="${AXERN_LOCAL_PROXY_URL:-http://host.docker.internal:7890}"
AXERN_LOCAL_PROXY_AUTODETECT="${AXERN_LOCAL_PROXY_AUTODETECT:-1}"

normalize_runsc_source() {
  case "$1" in
    auto|remote|local) echo "$1" ;;
    *)
      echo "unsupported RUNSC_SOURCE: $1 (expected one of: auto, remote, local)" >&2
      return 1
      ;;
  esac
}

normalize_mc_source() {
  case "$1" in
    auto|remote|local) echo "$1" ;;
    *)
      echo "unsupported MC_SOURCE: $1 (expected one of: auto, remote, local)" >&2
      return 1
      ;;
  esac
}

normalize_apt_mirror_source() {
  case "$1" in
    aliyun|ustc|tuna|archive) echo "$1" ;;
    *)
      echo "unsupported APT_MIRROR_SOURCE: $1 (expected one of: aliyun, ustc, tuna, archive)" >&2
      return 1
      ;;
  esac
}

normalize_cargo_registry_source() {
  case "$1" in
    aliyun|ustc|crates-io) echo "$1" ;;
    *)
      echo "unsupported CARGO_REGISTRY_SOURCE: $1 (expected one of: aliyun, ustc, crates-io)" >&2
      return 1
      ;;
  esac
}

APT_MIRROR_SOURCE="$(normalize_apt_mirror_source "${APT_MIRROR_SOURCE}")"
CARGO_REGISTRY_SOURCE="$(normalize_cargo_registry_source "${CARGO_REGISTRY_SOURCE}")"
RUNSC_SOURCE="$(normalize_runsc_source "${RUNSC_SOURCE}")"
MC_SOURCE="$(normalize_mc_source "${MC_SOURCE}")"

normalize_verify_docker_variant() {
  case "$1" in
    full|benchmark) echo "$1" ;;
    *)
      echo "unsupported VERIFY_DOCKER_VARIANT: $1 (expected one of: full, benchmark)" >&2
      return 1
      ;;
  esac
}

VERIFY_DOCKER_VARIANT="$(normalize_verify_docker_variant "${VERIFY_DOCKER_VARIANT}")"

if [ "${IMAGE_TAG}" = "axnoded-verify:latest" ] && [ "${VERIFY_DOCKER_VARIANT}" = "benchmark" ]; then
  IMAGE_TAG="axnoded-benchmark-verify:latest"
fi

normalize_docker_arch() {
  case "$1" in
    x86_64) echo "amd64" ;;
    aarch64) echo "arm64" ;;
    *) echo "$1" ;;
  esac
}

normalize_gvisor_arch() {
  case "$1" in
    arm64|aarch64) echo "aarch64" ;;
    amd64|x86_64) echo "x86_64" ;;
    *)
      echo "unsupported gVisor arch: $1" >&2
      return 1
      ;;
  esac
}

normalize_mc_arch() {
  case "$1" in
    arm64|aarch64) echo "arm64" ;;
    amd64|x86_64) echo "amd64" ;;
    *)
      echo "unsupported mc arch: $1" >&2
      return 1
      ;;
  esac
}

resolve_verify_docker_platform() {
  if [ -n "${VERIFY_DOCKER_PLATFORM:-}" ]; then
    printf '%s\n' "${VERIFY_DOCKER_PLATFORM}"
    return 0
  fi

  local server_os server_arch
  server_os="$(docker version --format '{{.Server.Os}}' 2>/dev/null || true)"
  server_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)"
  if [ -z "${server_os}" ]; then
    server_os="linux"
  fi
  if [ -z "${server_arch}" ]; then
    server_arch="$(uname -m)"
  fi
  server_arch="$(normalize_docker_arch "${server_arch}")"
  printf '%s/%s\n' "${server_os}" "${server_arch}"
}

resolve_runsc_cache_arch() {
  if [ -n "${RUNSC_CACHE_ARCH:-}" ]; then
    normalize_gvisor_arch "${RUNSC_CACHE_ARCH}"
    return 0
  fi

  local docker_arch=""
  if [ -n "${VERIFY_DOCKER_PLATFORM:-}" ]; then
    docker_arch="${VERIFY_DOCKER_PLATFORM##*/}"
  fi
  if [ -z "${docker_arch}" ]; then
    docker_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)"
  fi
  if [ -z "${docker_arch}" ]; then
    docker_arch="$(uname -m)"
  fi
  normalize_gvisor_arch "${docker_arch}"
}

resolve_runsc_source() {
  case "${RUNSC_SOURCE}" in
    local|remote)
      printf '%s\n' "${RUNSC_SOURCE}"
      return 0
      ;;
    auto)
      local cache_arch
      cache_arch="$(resolve_runsc_cache_arch)"
      RUNSC_CACHE_ARCH="${cache_arch}"
      if RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH}" "${ROOT_DIR}/scripts/cache/cache-runsc.sh" >/dev/null 2>&1; then
        printf '%s\n' "local"
        return 0
      fi
      printf '%s\n' "remote"
      return 0
      ;;
  esac
}

resolve_mc_cache_arch() {
  if [ -n "${MC_CACHE_ARCH:-}" ]; then
    normalize_mc_arch "${MC_CACHE_ARCH}"
    return 0
  fi

  local docker_arch=""
  if [ -n "${VERIFY_DOCKER_PLATFORM:-}" ]; then
    docker_arch="${VERIFY_DOCKER_PLATFORM##*/}"
  fi
  if [ -z "${docker_arch}" ]; then
    docker_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null || true)"
  fi
  if [ -z "${docker_arch}" ]; then
    docker_arch="$(uname -m)"
  fi
  normalize_mc_arch "${docker_arch}"
}

resolve_mc_source() {
  case "${MC_SOURCE}" in
    local|remote)
      printf '%s\n' "${MC_SOURCE}"
      return 0
      ;;
    auto)
      local cache_arch
      cache_arch="$(resolve_mc_cache_arch)"
      MC_CACHE_ARCH="${cache_arch}"
      if MC_CACHE_ARCH="${MC_CACHE_ARCH}" "${ROOT_DIR}/scripts/cache/cache-minio-mc.sh" >/dev/null 2>&1; then
        printf '%s\n' "local"
        return 0
      fi
      printf '%s\n' "remote"
      return 0
      ;;
  esac
}

resolve_perf_kernel_release() {
  if [ -n "${PERF_KERNEL_RELEASE:-}" ]; then
    printf '%s\n' "${PERF_KERNEL_RELEASE}"
    return 0
  fi

  local server_os kernel_release
  server_os="$(docker version --format '{{.Server.Os}}' 2>/dev/null || true)"
  kernel_release="$(docker info --format '{{.KernelVersion}}' 2>/dev/null || true)"
  if [ -n "${kernel_release}" ]; then
    printf '%s\n' "${kernel_release}"
    return 0
  fi
  if [ "${server_os}" = "linux" ] && [ "$(uname -s)" = "Linux" ]; then
    uname -r
    return 0
  fi
  printf '%s\n' ""
}

RUNSC_SOURCE="$(resolve_runsc_source)"
if [ -z "${RUNSC_CACHE_ARCH}" ]; then
  RUNSC_CACHE_ARCH="$(resolve_runsc_cache_arch)"
fi
MC_SOURCE="$(resolve_mc_source)"
if [ -z "${MC_CACHE_ARCH}" ]; then
  MC_CACHE_ARCH="$(resolve_mc_cache_arch)"
fi

resolve_docker_daemon_proxy() {
  local field="$1"
  docker info --format '{{json .}}' 2>/dev/null | jq -r --arg field "${field}" '.[$field] // empty' 2>/dev/null || true
}

resolve_default_local_proxy() {
  if [ "${AXERN_LOCAL_PROXY_AUTODETECT}" != "1" ] || [ -z "${AXERN_LOCAL_PROXY_URL}" ]; then
    printf '%s\n' ""
    return 0
  fi
  python3 - "${AXERN_LOCAL_PROXY_URL}" <<'PY'
import socket
import sys
from urllib.parse import urlparse

proxy = sys.argv[1]
parsed = urlparse(proxy)
host = parsed.hostname
port = parsed.port
if not host or not port:
    raise SystemExit(0)
probe_host = "127.0.0.1" if host in {"host.docker.internal", "localhost"} else host
sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
sock.settimeout(0.25)
try:
    sock.connect((probe_host, port))
except OSError:
    raise SystemExit(0)
finally:
    sock.close()
print(proxy)
PY
}

resolve_build_http_proxy() {
  if [ -n "${VERIFY_DOCKER_HTTP_PROXY:-}" ]; then
    printf '%s\n' "${VERIFY_DOCKER_HTTP_PROXY}"
    return 0
  fi
  local daemon_proxy
  daemon_proxy="$(resolve_docker_daemon_proxy "HttpProxy")"
  if [ -n "${daemon_proxy}" ] && [ "${daemon_proxy}" != "<no value>" ]; then
    printf '%s\n' "${daemon_proxy}"
    return 0
  fi
  if [ -n "${HTTP_PROXY:-${http_proxy:-}}" ]; then
    printf '%s\n' "${HTTP_PROXY:-${http_proxy:-}}"
    return 0
  fi
  resolve_default_local_proxy
}

reserve_host_port() {
  local host="$1"
  local preferred_port="$2"
  python3 - "$host" "$preferred_port" <<'PY'
import socket
import sys

host = sys.argv[1]
preferred = int(sys.argv[2])

def try_bind(port):
    family = socket.AF_INET6 if ":" in host and host != "0.0.0.0" else socket.AF_INET
    sock = socket.socket(family, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        sock.bind((host, port))
    except OSError:
        sock.close()
        return None
    actual = sock.getsockname()[1]
    sock.close()
    return actual

chosen = try_bind(preferred)
if chosen is None:
    chosen = try_bind(0)
    if chosen is None:
        raise SystemExit(1)
print(chosen)
PY
}

reserve_unique_host_port() {
  local host="$1"
  local preferred_port="$2"
  shift 2

  local candidate excluded duplicate
  while true; do
    candidate="$(reserve_host_port "${host}" "${preferred_port}")"
    duplicate=false
    for excluded in "$@"; do
      if [ -n "${excluded}" ] && [ "${candidate}" = "${excluded}" ]; then
        duplicate=true
        break
      fi
    done
    if [ "${duplicate}" = false ]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
    preferred_port=0
  done
}

node_summary_fresh() {
  local node_id="$1"
  local body="$2"
  python3 -c '
import json
import sys

node_id = sys.argv[1]
try:
    payload = json.load(sys.stdin)
except json.JSONDecodeError:
    raise SystemExit(1)

for item in payload.get("nodes", []):
    if item.get("node_id") != node_id:
        continue
    axnoded = ((item.get("summary") or {}).get("components") or {}).get("axnoded") or {}
    if (
        item.get("fresh") is True
        and item.get("summary_fresh") is True
        and axnoded.get("state") == 1
        and axnoded.get("ready") is True
    ):
        raise SystemExit(0)
raise SystemExit(1)
' "${node_id}" <<<"${body}"
}

import_oci_image_archive_to_node() {
  local image_ref="$1"
  local node_container_name="$2"
  local archive_in_node="$3"
  local image_archive payload

  image_archive="$(mktemp)"

  if ! docker save -o "${image_archive}" "${image_ref}"; then
    rm -f "${image_archive}"
    return 1
  fi
  if ! docker cp "${image_archive}" "${node_container_name}:${archive_in_node}"; then
    rm -f "${image_archive}"
    return 1
  fi
  payload="$(python3 - "${image_ref}" "${archive_in_node}" <<'PY'
import json
import sys

image_ref, archive_path = sys.argv[1], sys.argv[2]
print(json.dumps({"image_ref": image_ref, "archive_path": archive_path}))
PY
)"
  if ! printf '%s' "${payload}" | docker exec -i "${node_container_name}" curl \
    -fsS \
    --unix-socket /run/imagemgr/imagemgr.sock \
    -H "Content-Type: application/json" \
    -d @- \
    http://unix/oci_import >/dev/null; then
    docker exec "${node_container_name}" rm -f "${archive_in_node}" >/dev/null 2>&1 || true
    rm -f "${image_archive}"
    return 1
  fi
  docker exec "${node_container_name}" rm -f "${archive_in_node}" >/dev/null 2>&1 || true
  rm -f "${image_archive}"
}

prepare_oci_test_image_source() {
  local image_ref="$1"
  local source_mode="${OCI_TEST_IMAGE_SOURCE:-auto}"
  local local_registry_port="${OCI_TEST_LOCAL_REGISTRY_PORT:-5001}"
  local local_registry_name="${LOCAL_REGISTRY_NAME:-axern-registry}"
  local image_id image_tag host_ref registry_ip runtime_ref

  PREPARED_OCI_TEST_IMAGE="${image_ref}"
  OCI_TEST_INSECURE_REGISTRIES="${IMAGEMGR_INSECURE_REGISTRIES:-}"

  case "${source_mode}" in
    registry)
      echo "oci_test_image_source=registry image=${image_ref}"
      return 0
      ;;
    auto)
      if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
        echo "oci_test_image_source=registry image=${image_ref} reason=not-in-docker-cache"
        return 0
      fi
      ;;
    docker-cache)
      if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
        echo "OCI test image is not available in the Docker cache: ${image_ref}" >&2
        echo "Pull the image first or set OCI_TEST_IMAGE_SOURCE=registry." >&2
        return 1
      fi
      ;;
    *)
      echo "unsupported OCI_TEST_IMAGE_SOURCE: ${source_mode} (expected auto, docker-cache, or registry)" >&2
      return 1
      ;;
  esac

  if ! LOCAL_REGISTRY_PORT="${local_registry_port}" "${REPO_ROOT}/scripts/dev-env/registry-up.sh" >/dev/null; then
    echo "failed to start the repo-managed local registry on port ${local_registry_port}" >&2
    return 1
  fi

  image_id="$(docker image inspect --format '{{.Id}}' "${image_ref}")"
  image_tag="${image_id#sha256:}"
  image_tag="${image_tag:0:16}"
  host_ref="localhost:${local_registry_port}/axern/oci-e2e:${image_tag}"

  docker tag "${image_ref}" "${host_ref}"
  docker push "${host_ref}" >/dev/null

  registry_ip="$(docker inspect --format '{{with index .NetworkSettings.Networks "bridge"}}{{.IPAddress}}{{end}}' "${local_registry_name}")"
  if [ -z "${registry_ip}" ]; then
    echo "local registry has no bridge address: ${local_registry_name}" >&2
    return 1
  fi
  runtime_ref="${registry_ip}:5000/axern/oci-e2e:${image_tag}"
  case ",${REGISTRY_NO_PROXY}," in
    *,"${registry_ip}",*) ;;
    *) REGISTRY_NO_PROXY="${REGISTRY_NO_PROXY:+${REGISTRY_NO_PROXY},}${registry_ip}" ;;
  esac

  PREPARED_OCI_TEST_IMAGE="${runtime_ref}"
  if [ -n "${OCI_TEST_INSECURE_REGISTRIES}" ]; then
    OCI_TEST_INSECURE_REGISTRIES+=",${runtime_ref%%/*}"
  else
    OCI_TEST_INSECURE_REGISTRIES="${runtime_ref%%/*}"
  fi
  export PREPARED_OCI_TEST_IMAGE OCI_TEST_INSECURE_REGISTRIES REGISTRY_NO_PROXY
  echo "oci_test_image_source=docker-cache original_image=${image_ref} runtime_image=${runtime_ref}"
}

prepare_nydus_test_image_source() {
  local image_ref="$1"
  local source_mode="${NYDUS_TEST_IMAGE_SOURCE:-local-build}"
  local build_output runtime_ref

  PREPARED_NYDUS_TEST_IMAGE="${image_ref}"
  OCI_TEST_INSECURE_REGISTRIES="${IMAGEMGR_INSECURE_REGISTRIES:-${OCI_TEST_INSECURE_REGISTRIES:-}}"

  case "${source_mode}" in
    registry)
      echo "nydus_test_image_source=registry image=${image_ref}"
      export PREPARED_NYDUS_TEST_IMAGE OCI_TEST_INSECURE_REGISTRIES
      return 0
      ;;
    local-build)
      build_output="$(NYDUS_PLATFORM="${VERIFY_DOCKER_PLATFORM}" bash "${REPO_ROOT}/scripts/dev-env/registry-nydus-image-build.sh")"
      runtime_ref="$(printf '%s\n' "${build_output}" | awk -F= '$1 == "cluster_nydus_image" {print $2}')"
      if [ -z "${runtime_ref}" ]; then
        printf '%s\n' "${build_output}" >&2
        echo "local Nydus image build did not return cluster_nydus_image" >&2
        return 1
      fi
      ;;
    *)
      echo "unsupported NYDUS_TEST_IMAGE_SOURCE: ${source_mode} (expected local-build or registry)" >&2
      return 1
      ;;
  esac

  if [ -n "${OCI_TEST_INSECURE_REGISTRIES}" ]; then
    OCI_TEST_INSECURE_REGISTRIES+=",${runtime_ref%%/*}"
  else
    OCI_TEST_INSECURE_REGISTRIES="${runtime_ref%%/*}"
  fi

  PREPARED_NYDUS_TEST_IMAGE="${runtime_ref}"
  export PREPARED_NYDUS_TEST_IMAGE OCI_TEST_INSECURE_REGISTRIES REGISTRY_NO_PROXY
  echo "nydus_test_image_source=local-build runtime_image=${runtime_ref}"
}

resolve_build_https_proxy() {
  if [ -n "${VERIFY_DOCKER_HTTPS_PROXY:-}" ]; then
    printf '%s\n' "${VERIFY_DOCKER_HTTPS_PROXY}"
    return 0
  fi
  local daemon_proxy
  daemon_proxy="$(resolve_docker_daemon_proxy "HttpsProxy")"
  if [ -n "${daemon_proxy}" ] && [ "${daemon_proxy}" != "<no value>" ]; then
    printf '%s\n' "${daemon_proxy}"
    return 0
  fi
  if [ -n "${HTTPS_PROXY:-${https_proxy:-}}" ]; then
    printf '%s\n' "${HTTPS_PROXY:-${https_proxy:-}}"
    return 0
  fi
  resolve_default_local_proxy
}

resolve_build_no_proxy() {
  if [ -n "${VERIFY_DOCKER_NO_PROXY:-}" ]; then
    printf '%s\n' "${VERIFY_DOCKER_NO_PROXY}"
    return 0
  fi
  local daemon_proxy
  daemon_proxy="$(resolve_docker_daemon_proxy "NoProxy")"
  if [ -n "${daemon_proxy}" ] && [ "${daemon_proxy}" != "<no value>" ]; then
    printf '%s\n' "${daemon_proxy}"
    return 0
  fi
  printf '%s\n' "${NO_PROXY:-${no_proxy:-}}"
}

normalize_runtime_proxy_url() {
  local proxy_url="$1"
  if [ -z "${proxy_url}" ]; then
    printf '%s\n' ""
    return 0
  fi
  printf '%s\n' "${proxy_url}" | sed -E 's#://(localhost|127\.0\.0\.1|\[::1\])([:/]|$)#://host.docker.internal\2#g'
}

normalize_host_proxy_url() {
  local proxy_url="$1"
  if [ -z "${proxy_url}" ]; then
    printf '%s\n' ""
    return 0
  fi
  printf '%s\n' "${proxy_url}" | sed -E 's#://host\.docker\.internal([:/]|$)#://127.0.0.1\1#g'
}

resolve_runtime_registry_proxy() {
  local proxy_url=""
  proxy_url="$(resolve_build_https_proxy)"
  if [ -z "${proxy_url}" ]; then
    proxy_url="$(resolve_build_http_proxy)"
  fi
  normalize_runtime_proxy_url "${proxy_url}"
}

resolve_runtime_registry_no_proxy() {
  local base_no_proxy=""
  local defaults="localhost,127.0.0.1,127.0.0.0/8,::1,host.docker.internal,oss"
  base_no_proxy="$(resolve_build_no_proxy)"
  if [ -n "${base_no_proxy}" ]; then
    printf '%s,%s\n' "${base_no_proxy}" "${defaults}"
    return 0
  fi
  printf '%s\n' "${defaults}"
}

if [ -z "${REGISTRY_PROXY_URL+x}" ]; then
  REGISTRY_PROXY_URL="$(resolve_runtime_registry_proxy)"
fi
if [ -z "${REGISTRY_NO_PROXY+x}" ]; then
  REGISTRY_NO_PROXY="$(resolve_runtime_registry_no_proxy)"
fi
export REGISTRY_PROXY_URL REGISTRY_NO_PROXY

build_verify_image() {
  local apt_mirror_source="$1"
  local cargo_registry_source="$2"
  local dockerfile_path="${ROOT_DIR}/docker/verify/Dockerfile"
  local perf_kernel_release=""
  local build_http_proxy=""
  local build_https_proxy=""
  local build_no_proxy=""
  if [ "${VERIFY_DOCKER_VARIANT}" = "benchmark" ]; then
    dockerfile_path="${ROOT_DIR}/docker/benchmark/Dockerfile"
    perf_kernel_release="$(resolve_perf_kernel_release)"
  fi
  build_http_proxy="$(resolve_build_http_proxy)"
  build_https_proxy="$(resolve_build_https_proxy)"
  build_no_proxy="$(resolve_build_no_proxy)"
  local build_args=(
    -f "${dockerfile_path}"
    --build-arg GO_IMAGE="${GO_IMAGE}"
    --build-arg BASE_IMAGE="${BASE_IMAGE}"
    --build-arg NGINX_IMAGE="${NGINX_IMAGE}"
    --build-arg NODE_RUNTIME_BASE_IMAGE="${NODE_RUNTIME_BASE_IMAGE_TAG}"
    --build-arg APT_MIRROR_SOURCE="${apt_mirror_source}"
    --build-arg CARGO_REGISTRY_SOURCE="${cargo_registry_source}"
    --build-arg GOPROXY="${GOPROXY}"
    --build-arg GOSUMDB="${GOSUMDB}"
    --build-arg RUNSC_SOURCE="${RUNSC_SOURCE}"
    --build-arg RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH}"
    --build-arg MC_SOURCE="${MC_SOURCE}"
    --build-arg MC_CACHE_ARCH="${MC_CACHE_ARCH}"
    -t "${IMAGE_TAG}"
  )
  if [ -n "${build_http_proxy}" ]; then
    build_args+=(
      --build-arg HTTP_PROXY="${build_http_proxy}"
      --build-arg http_proxy="${build_http_proxy}"
    )
  fi
  if [ -n "${build_https_proxy}" ]; then
    build_args+=(
      --build-arg HTTPS_PROXY="${build_https_proxy}"
      --build-arg https_proxy="${build_https_proxy}"
    )
  fi
  if [ -n "${build_no_proxy}" ]; then
    build_args+=(
      --build-arg NO_PROXY="${build_no_proxy}"
      --build-arg no_proxy="${build_no_proxy}"
    )
  fi
  if [ -n "${perf_kernel_release}" ]; then
    build_args+=(
      --build-arg PERF_KERNEL_RELEASE="${perf_kernel_release}"
    )
  fi
  if [ -n "${VERIFY_DOCKER_PLATFORM}" ]; then
    build_args=(--platform "${VERIFY_DOCKER_PLATFORM}" "${build_args[@]}")
  fi
  if [ "${VERIFY_DOCKER_PULL:-false}" = "true" ]; then
    build_args=(--pull "${build_args[@]}")
  fi

  if [ -n "${VERIFY_DOCKER_PLATFORM}" ] && docker buildx version >/dev/null 2>&1; then
    docker buildx build \
      --load \
      "${build_args[@]}" \
      "${REPO_ROOT}"
    return $?
  fi

  local buildkit="${VERIFY_DOCKER_BUILDKIT}"
  if [ -n "${VERIFY_DOCKER_PLATFORM}" ]; then
    buildkit=1
  fi
  if [ "${buildkit}" != "1" ]; then
    echo "VERIFY_DOCKER_BUILDKIT=0 is no longer supported by the verify image; enable BuildKit to use cache-mounted multi-stage builds" >&2
    return 1
  fi

  DOCKER_BUILDKIT="${buildkit}" docker build \
    "${build_args[@]}" \
    "${REPO_ROOT}"
}

build_node_runtime_base_image() {
  local apt_mirror_source="$1"
  local cargo_registry_source="$2"
  local dockerfile_path="${REPO_ROOT}/deploy/images/lib/node-runtime-base.Dockerfile"
  local build_http_proxy=""
  local build_https_proxy=""
  local build_no_proxy=""
  build_http_proxy="$(resolve_build_http_proxy)"
  build_https_proxy="$(resolve_build_https_proxy)"
  build_no_proxy="$(resolve_build_no_proxy)"
  local build_args=(
    -f "${dockerfile_path}"
    --build-arg GO_IMAGE="${GO_IMAGE}"
    --build-arg RUST_IMAGE="${RUST_IMAGE}"
    --build-arg BASE_IMAGE="${BASE_IMAGE}"
    --build-arg APT_MIRROR_SOURCE="${apt_mirror_source}"
    --build-arg CARGO_REGISTRY_SOURCE="${cargo_registry_source}"
    --build-arg GOPROXY="${GOPROXY}"
    --build-arg GOSUMDB="${GOSUMDB}"
    --build-arg RUNSC_SOURCE="${RUNSC_SOURCE}"
    --build-arg RUNSC_CACHE_ARCH="${RUNSC_CACHE_ARCH}"
    --build-arg MC_SOURCE="${MC_SOURCE}"
    --build-arg MC_CACHE_ARCH="${MC_CACHE_ARCH}"
    -t "${NODE_RUNTIME_BASE_IMAGE_TAG}"
  )
  if [ -n "${build_http_proxy}" ]; then
    build_args+=(
      --build-arg HTTP_PROXY="${build_http_proxy}"
      --build-arg http_proxy="${build_http_proxy}"
    )
  fi
  if [ -n "${build_https_proxy}" ]; then
    build_args+=(
      --build-arg HTTPS_PROXY="${build_https_proxy}"
      --build-arg https_proxy="${build_https_proxy}"
    )
  fi
  if [ -n "${build_no_proxy}" ]; then
    build_args+=(
      --build-arg NO_PROXY="${build_no_proxy}"
      --build-arg no_proxy="${build_no_proxy}"
    )
  fi
  if [ -n "${VERIFY_DOCKER_PLATFORM}" ]; then
    build_args=(--platform "${VERIFY_DOCKER_PLATFORM}" "${build_args[@]}")
  fi
  if [ "${VERIFY_DOCKER_PULL:-false}" = "true" ]; then
    build_args=(--pull "${build_args[@]}")
  fi

  if [ -n "${VERIFY_DOCKER_PLATFORM}" ] && docker buildx version >/dev/null 2>&1; then
    docker buildx build \
      --load \
      "${build_args[@]}" \
      "${REPO_ROOT}"
    return $?
  fi

  local buildkit="${VERIFY_DOCKER_BUILDKIT}"
  if [ -n "${VERIFY_DOCKER_PLATFORM}" ]; then
    buildkit=1
  fi
  if [ "${buildkit}" != "1" ]; then
    echo "VERIFY_DOCKER_BUILDKIT=0 is no longer supported by the shared node runtime base image" >&2
    return 1
  fi

  DOCKER_BUILDKIT="${buildkit}" axern_docker_build \
    "${build_args[@]}" \
    "${REPO_ROOT}"
}

ensure_verify_image() {
  local apt_mirror_source="${APT_MIRROR_SOURCE}"
  local cargo_registry_source="${CARGO_REGISTRY_SOURCE}"

  if [ "${VERIFY_DOCKER_VARIANT}" = "full" ]; then
    if build_node_runtime_base_image "${apt_mirror_source}" "${cargo_registry_source}"; then
      :
    elif [ "${cargo_registry_source}" != "crates-io" ]; then
      echo "shared node runtime base build failed with APT_MIRROR_SOURCE=${apt_mirror_source} and CARGO_REGISTRY_SOURCE=${cargo_registry_source}, retrying with CARGO_REGISTRY_SOURCE=crates-io" >&2
      cargo_registry_source="crates-io"
      if build_node_runtime_base_image "${apt_mirror_source}" "${cargo_registry_source}"; then
        :
      elif [ "${apt_mirror_source}" != "archive" ]; then
        echo "shared node runtime base build failed with APT_MIRROR_SOURCE=${apt_mirror_source} and CARGO_REGISTRY_SOURCE=${cargo_registry_source}, retrying with APT_MIRROR_SOURCE=archive" >&2
        apt_mirror_source="archive"
        build_node_runtime_base_image "${apt_mirror_source}" "${cargo_registry_source}"
      else
        exit 1
      fi
    elif [ "${apt_mirror_source}" != "archive" ]; then
      echo "shared node runtime base build failed with APT_MIRROR_SOURCE=${apt_mirror_source} and CARGO_REGISTRY_SOURCE=${cargo_registry_source}, retrying with APT_MIRROR_SOURCE=archive" >&2
      apt_mirror_source="archive"
      build_node_runtime_base_image "${apt_mirror_source}" "${cargo_registry_source}"
    else
      exit 1
    fi
  fi

  if build_verify_image "${apt_mirror_source}" "${cargo_registry_source}"; then
    return 0
  fi

  if [ "${cargo_registry_source}" != "crates-io" ]; then
    echo "docker build failed with APT_MIRROR_SOURCE=${apt_mirror_source} and CARGO_REGISTRY_SOURCE=${cargo_registry_source}, retrying with CARGO_REGISTRY_SOURCE=crates-io" >&2
    cargo_registry_source="crates-io"
    if build_verify_image "${apt_mirror_source}" "${cargo_registry_source}"; then
      return 0
    fi
  fi

  if [ "${apt_mirror_source}" != "archive" ]; then
    echo "docker build failed with APT_MIRROR_SOURCE=${apt_mirror_source} and CARGO_REGISTRY_SOURCE=${cargo_registry_source}, retrying with APT_MIRROR_SOURCE=archive" >&2
    build_verify_image archive "${cargo_registry_source}"
    return $?
  fi

  exit 1
}

cleanup_verify_container() {
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}

run_verify_container() {
  if [ "$#" -eq 0 ]; then
    echo "run_verify_container requires a command" >&2
    exit 1
  fi

  cleanup_verify_container

  local run_args=(
    --name "${CONTAINER_NAME}"
    --privileged
  )
  local passthrough_envs=(
    RUNTIME_UNDER_TEST
    RUNTIME_BINARY
    SOCKET_ADDRESS
    REGISTRY_PROXY_URL
    REGISTRY_NO_PROXY
    DEBUG_LOG_LINES
    NAT_BACKEND
    AXNODED_IP_RANGE
    AXNODED_SNAT_CIDR
    VERIFY_SKIP_LOCALHOST
    BENCHMARK_REQUESTS
    BENCHMARK_CONCURRENCY
    BENCHMARK_WARMUP_REQUESTS
    BENCHMARK_PATHS
    BENCHMARK_PROFILE_MODE
    BENCHMARK_PROFILE_EVENTS
    BENCHMARK_PROFILE_RETRIES
    STARTUP_MATRIX_SCENARIO
    STARTUP_MATRIX_MODE
    STARTUP_MATRIX_SAMPLES
  )
  local env_name
  for env_name in "${passthrough_envs[@]}"; do
    if [ -n "${!env_name:-}" ]; then
      run_args+=(-e "${env_name}=${!env_name}")
    fi
  done
  if [ -n "${VERIFY_DOCKER_PLATFORM}" ]; then
    run_args+=(--platform "${VERIFY_DOCKER_PLATFORM}")
  fi

  docker run --rm \
    "${run_args[@]}" \
    "${IMAGE_TAG}" \
    "$@"
}
