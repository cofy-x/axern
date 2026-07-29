#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
version="$(tr -d '[:space:]' < "${AXERN_ROOT}/VERSION")"
mode="${1:-}"
case "${mode}" in
  candidate|published) ;;
  *) echo "usage: $0 candidate|published" >&2; exit 2 ;;
esac

for name in AXERN_SDK_ACCEPTANCE_CONFIG AXERN_SDK_ACCEPTANCE_CONTEXT AXERN_SDK_ACCEPTANCE_CLI; do
  if [ -z "${!name:-}" ]; then
    echo "${name} is required" >&2
    exit 2
  fi
done
export AXERN_SDK_ACCEPTANCE_VERSION="${version}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
handshake_dir="${tmp_dir}/handshake"
mkdir -p "${handshake_dir}"
export AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR="${handshake_dir}"

retry() {
  local attempt=1
  local max_attempts=20
  until "$@"; do
    if [ "${attempt}" -ge "${max_attempts}" ]; then
      echo "command did not succeed after ${max_attempts} attempts: $*" >&2
      return 1
    fi
    sleep "$((attempt < 5 ? attempt : 5))"
    attempt=$((attempt + 1))
  done
}

run_sdk() {
  local language="$1"
  shift
  local service_file="${handshake_dir}/${language}.service-id"
  local verified_file="${handshake_dir}/${language}.verified"
  local sdk_pid=""
  rm -f "${service_file}" "${verified_file}"
  "$@" &
  sdk_pid=$!
  for _ in $(seq 1 1200); do
    if [ -s "${service_file}" ]; then
      break
    fi
    if ! kill -0 "${sdk_pid}" 2>/dev/null; then
      wait "${sdk_pid}"
      return $?
    fi
    sleep 0.25
  done
  if [ ! -s "${service_file}" ]; then
    echo "${language} SDK did not publish a service id" >&2
    touch "${verified_file}"
    wait "${sdk_pid}" || true
    return 1
  fi

  local service_id
  service_id="$(tr -d '[:space:]' < "${service_file}")"
  local service_json
  if ! service_json="$("${AXERN_SDK_ACCEPTANCE_CLI}" \
    --config "${AXERN_SDK_ACCEPTANCE_CONFIG}" \
    --context "${AXERN_SDK_ACCEPTANCE_CONTEXT}" \
    service get "${service_id}" --output json)"; then
    touch "${verified_file}"
    wait "${sdk_pid}" || true
    return 1
  fi
  if ! python3 -c '
import json
import sys

expected = sys.argv[1]
document = json.load(sys.stdin)
if document.get("service", {}).get("id") != expected:
    raise SystemExit("Axern CLI did not observe the SDK service")
' "${service_id}" <<<"${service_json}"; then
    touch "${verified_file}"
    wait "${sdk_pid}" || true
    return 1
  fi
  touch "${verified_file}"
  wait "${sdk_pid}"
}

python_spec="axern-sdk==${version}"
typescript_spec="@cofy-x/axern-sdk@${version}"
go_replace=""
if [ "${mode}" = candidate ]; then
  dist="${AXERN_SDK_DIST:-${AXERN_ROOT}/dist/sdk}"
  python_spec="${dist}/python/axern_sdk-${version}-py3-none-any.whl"
  typescript_spec="${dist}/typescript/cofy-x-axern-sdk-${version}.tgz"
  go_replace="replace github.com/cofy-x/axern/sdk/go => ${AXERN_ROOT}/sdk/go"
fi

python_dir="${tmp_dir}/python"
uv venv --python 3.12 "${python_dir}"
python_install=(uv pip install --python "${python_dir}/bin/python" "${python_spec}")
if [ "${mode}" = published ]; then
  python_install=(uv pip install --python "${python_dir}/bin/python" --refresh-package axern-sdk "${python_spec}")
fi
retry "${python_install[@]}"
run_sdk python "${python_dir}/bin/python" "${AXERN_ROOT}/scripts/release/sdk-data-plane/python.py"

typescript_dir="${tmp_dir}/typescript"
mkdir -p "${typescript_dir}"
retry npm install --prefix "${typescript_dir}" --ignore-scripts --no-audit --no-fund "${typescript_spec}"
cp "${AXERN_ROOT}/scripts/release/sdk-data-plane/typescript.mjs" "${typescript_dir}/acceptance.mjs"
run_sdk typescript node "${typescript_dir}/acceptance.mjs"

go_dir="${tmp_dir}/go"
mkdir -p "${go_dir}"
cat > "${go_dir}/go.mod" <<EOF
module example.com/axern-sdk-data-plane-acceptance

go 1.25.12

require github.com/cofy-x/axern/sdk/go v${version}
${go_replace}
EOF
cp "${AXERN_ROOT}/scripts/release/sdk-data-plane/main.go" "${go_dir}/main.go"
if [ "${mode}" = published ]; then
  retry env GOPROXY=https://proxy.golang.org,direct go -C "${go_dir}" mod tidy
else
  go -C "${go_dir}" mod tidy
fi
run_sdk go go -C "${go_dir}" run .

echo "sdk_data_plane_acceptance_ok=${mode} version=${version}"
