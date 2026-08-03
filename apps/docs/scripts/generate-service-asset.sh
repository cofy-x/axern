#!/usr/bin/env bash
set -euo pipefail

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
docs_dir=$(CDPATH='' cd -- "${script_dir}/.." && pwd)
repo_dir=$(CDPATH='' cd -- "${docs_dir}/../.." && pwd)
context=${AXERN_DOCS_CONTEXT:-compose}
config=${AXERN_CONFIG:-${HOME}/.config/axern/config.json}
service_url=${AXERN_SERVICE_URL:-http://127.0.0.1:25080}

command -v vhs >/dev/null 2>&1 || {
  echo "missing vhs: install https://github.com/charmbracelet/vhs" >&2
  exit 1
}
command -v uv >/dev/null 2>&1 || {
  echo "missing uv: install https://docs.astral.sh/uv/" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "missing python3: install Python 3" >&2
  exit 1
}
test -f "${config}" || {
  echo "missing Axern context file: ${config}" >&2
  exit 1
}
test -x "${repo_dir}/bin/axern" || {
  echo "missing ${repo_dir}/bin/axern; run make axern-cli-build" >&2
  exit 1
}
curl --noproxy 'localhost,127.0.0.1,::1' --connect-timeout 2 --max-time 5 \
  --fail --silent --show-error "${service_url%/}/healthz" >/dev/null || {
  echo "local Axern gateway is not ready at ${service_url}; run axern local up" >&2
  exit 1
}

resource_counts() {
  local service_count environment_count
  service_count=$("${repo_dir}/bin/axern" --context "${context}" service list \
    --namespace default \
    --label axern.example=python-sdk-service-gateway \
    --output json | python3 -c 'import json, sys; print(len(json.load(sys.stdin)["services"]))')
  environment_count=$("${repo_dir}/bin/axern" --context "${context}" environment list \
    --output json | python3 -c '
import json
import sys

items = json.load(sys.stdin).get("environments", [])
print(sum(
    item.get("labels", {}).get("axern.example") == "python-sdk-service-gateway"
    and item.get("status") != "deleted"
    for item in items
))
')
  printf '%s %s\n' "${service_count}" "${environment_count}"
}

read -r service_count environment_count < <(resource_counts)
if [[ "${service_count}" != 0 || "${environment_count}" != 0 ]]; then
  echo "Service recording requires a clean example scope: services=${service_count} environments=${environment_count}" >&2
  exit 1
fi

mkdir -p "${docs_dir}/public/terminal"
export AXERN_CONFIG="${config}"
export AXERN_CONTEXT="${context}"
export AXERN_SERVICE_URL="${service_url}"
export AXERN_CLI_BINARY="${repo_dir}/bin/axern"
export AXERN_DOCS_RECORDING=1

cd "${repo_dir}"
vhs "${docs_dir}/vhs/python-service.tape"

read -r service_count environment_count < <(resource_counts)
if [[ "${service_count}" != 0 || "${environment_count}" != 0 ]]; then
  echo "Service recording leaked resources: services=${service_count} environments=${environment_count}" >&2
  exit 1
fi

echo "docs_service_recording_cleanup_ok=true"
