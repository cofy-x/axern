#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd openssl
require_cmd python3

make -C "${AXERN_ROOT}" axrun-build >/dev/null
axrun="${AXERN_ROOT}/bin/axrun"
config_file="$(axern_config_file)"
base_compose="${DEPLOY_ROOT}/compose/docker-compose.yml"
managed_compose="${DEPLOY_ROOT}/compose/docker-compose.managed-rollout.yml"
compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${base_compose}" -f "${managed_compose}")
fixture="${COMPOSE_STATE_DIR}/managed-rollout-fixture"
run_suffix="$(date +%s)-$$"

finish() {
  status=$?
  trap - EXIT
  if [[ "${status}" -ne 0 ]]; then
    echo "managed_rollout_diagnostics=true" >&2
    docker compose "${compose_args[@]}" ps >&2 || true
    echo "--- node capacity ---" >&2
    curl --noproxy 'localhost,127.0.0.1,::1' --connect-timeout 2 --max-time 5 -fsS \
      "http://127.0.0.1:${COMPOSE_CONTROLD_HTTP_PORT}/nodesz" | python3 -c '
import json
import sys

for node in json.load(sys.stdin).get("nodes", []):
    summary = node.get("summary") or {}
    print(json.dumps({
        "node_id": node.get("node_id"),
        "fresh": node.get("fresh"),
        "summary_fresh": node.get("summary_fresh"),
        "capacity": summary.get("capacity"),
        "allocatable": summary.get("allocatable"),
        "resources": summary.get("resources"),
        "pools": summary.get("pools"),
        "components": summary.get("components"),
    }, sort_keys=True))
' >&2 || true
    docker stats --no-stream --format 'container={{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}}' \
      "${COMPOSE_PROJECT_NAME}-node-1" \
      "${COMPOSE_PROJECT_NAME}-controld-1" \
      "${COMPOSE_PROJECT_NAME}-rollout-worker-1" >&2 || true
    echo "--- allocation diagnostics ---" >&2
    docker exec "${COMPOSE_PROJECT_NAME}-node-1" sh -lc '
      count=$(grep -c "NodeLifecycle/CreateAllocation" /var/log/axnoded/axnoded.log 2>/dev/null || true)
      echo "create_allocation_request_count=${count:-0}"
      grep -E "level=(error|warning)|loaded runtime handler|prepared copy-on-write workspace image|executed runtime command.*error=true|reporting exited allocation" \
        /var/log/axnoded/axnoded.log 2>/dev/null | \
        grep -Eiv "raw-request|token|authorization|bearer" | tail -n 160 || true

      redact_pattern="raw-request|token|authorization|bearer|api[_-]?key|password|secret|credential|ticket|presigned"
      echo "--- allocation stderr ---"
      for container_dir in /var/lib/axnoded/root/containers/alloc-*; do
        [ -d "${container_dir}" ] || continue
        allocation_id=${container_dir##*/}
        stderr_path=${container_dir}/stderr.log
        if [ ! -s "${stderr_path}" ]; then
          echo "allocation_stderr=${allocation_id} empty_or_missing=true"
          continue
        fi
        echo "allocation_stderr=${allocation_id}"
        tail -n 120 "${stderr_path}" | grep -Eiv "${redact_pattern}" || true
      done

      echo "--- runsc exit states ---"
      for state_path in /var/lib/axnoded/root/runtime-exit-states/runsc/alloc-*.json; do
        [ -s "${state_path}" ] || continue
        allocation_id=${state_path##*/}
        printf "runsc_exit_state=%s " "${allocation_id%.json}"
        tr "\n" " " < "${state_path}" | grep -Eiv "${redact_pattern}" || true
        echo
      done

      echo "--- runsc containers ---"
      /usr/local/bin/runsc --root /var/lib/axnoded/root/runsc list --format json 2>&1 | \
        grep -Eiv "${redact_pattern}" || true
    ' >&2 || true
    docker compose "${compose_args[@]}" logs --no-color --tail=160 \
      controld rollout-worker node gatewayd tunneld mock-provider >&2 || true
    for failed_rollout_id in "${rollout_id:-}" "${run_id:-}" "${claude_rollout_id:-}"; do
      if [[ -n "${failed_rollout_id}" ]]; then
        "${axrun}" --config "${config_file}" --format json rollout get "${failed_rollout_id}" >&2 || true
        "${axrun}" --config "${config_file}" rollout inspect "${failed_rollout_id}" >&2 || true
      fi
    done
  fi
  end_env_lock compose || true
  exit "${status}"
}
trap finish EXIT

export MANAGED_ROLLOUT_MOCK_PROVIDER_PORT="${MANAGED_ROLLOUT_MOCK_PROVIDER_PORT:-25443}"
mock_provider_host_ip="$(docker compose "${compose_args[@]}" exec -T node \
  getent hosts host.docker.internal | awk 'NR == 1 {print $1}')"
python3 - "${mock_provider_host_ip}" <<'PY'
import ipaddress
import sys

ipaddress.ip_address(sys.argv[1])
PY
if [[ "$(uname -s)" == "Linux" ]]; then
  default_mock_provider_bind_address="${mock_provider_host_ip}"
else
  default_mock_provider_bind_address=127.0.0.1
fi
export MANAGED_ROLLOUT_MOCK_PROVIDER_BIND_ADDRESS="${MANAGED_ROLLOUT_MOCK_PROVIDER_BIND_ADDRESS:-${default_mock_provider_bind_address}}"
mock_provider_base_url="https://${mock_provider_host_ip}:${MANAGED_ROLLOUT_MOCK_PROVIDER_PORT}"

mock_provider_certs="${COMPOSE_STATE_DIR}/mock-provider-certs"
rm -rf "${mock_provider_certs}"
mkdir -p "${mock_provider_certs}"
openssl req -new -newkey rsa:2048 -nodes \
  -subj /CN=axern-managed-rollout-mock \
  -keyout "${mock_provider_certs}/server.key" \
  -out "${mock_provider_certs}/server.csr" >/dev/null 2>&1
printf 'subjectAltName=IP:%s\nextendedKeyUsage=serverAuth\n' "${mock_provider_host_ip}" > "${mock_provider_certs}/server.ext"
openssl x509 -req -sha256 -days 30 \
  -in "${mock_provider_certs}/server.csr" \
  -CA "${COMPOSE_STATE_DIR}/certs/ca.crt" \
  -CAkey "${COMPOSE_STATE_DIR}/certs/ca.key" \
  -set_serial 1 \
  -extfile "${mock_provider_certs}/server.ext" \
  -out "${mock_provider_certs}/server.crt" >/dev/null 2>&1

rm -rf "${fixture}"
mkdir -p "${fixture}"
registry_certs="${COMPOSE_STATE_DIR}/registry-certs/registry:5000"
mkdir -p "${registry_certs}"
cp "${COMPOSE_STATE_DIR}/certs/ca.crt" "${registry_certs}/ca.crt"
mock_runtime_image="axern/managed-rollout-mock-runtime:dev"
docker build \
  --build-arg "PYTHON_RUNTIME_IMAGE=${PYTHON311_RUNTIME_IMAGE}" \
  -f "${DEPLOY_ROOT}/compose/mock-provider-runtime.Dockerfile" \
  -t "${mock_runtime_image}" \
  "${COMPOSE_STATE_DIR}/certs" >/dev/null
mock_runtime_id="$(docker image inspect "${mock_runtime_image}" --format '{{.Id}}')"
mock_runtime_ref="index.docker.io/axern/managed-rollout-mock-runtime@${mock_runtime_id}"
"${axrun}" task init --output-dir "${fixture}/source" >/dev/null
python3 - "${fixture}/source/taskset.yaml" "${mock_runtime_ref}" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
runtime_ref = sys.argv[2]
text = path.read_text()
text = text.replace("Create answer.txt in the workspace.", "Create scripted-agent.txt in the workspace using the available shell tool.")
text = text.replace("- path: answer.txt", "- path: scripted-agent.txt")
text = text.replace("type: template\n                    template_id: python311", f"type: image\n                    image: {runtime_ref}")
path.write_text(text)
PY
python3 - "${fixture}/source/verifier/check.sh" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
path.write_text("#!/usr/bin/env bash\nset -euo pipefail\ngrep -Fx 'managed rollout mock provider ok' /workspace/scripted-agent.txt\n")
path.chmod(0o755)
PY
"${axrun}" task build --file "${fixture}/source/taskset.yaml" --output "${fixture}/bundle" >/dev/null

docker compose "${compose_args[@]}" up -d registry minio-rollout-bootstrap
docker compose "${compose_args[@]}" up -d --force-recreate mock-provider
docker compose "${compose_args[@]}" up -d --force-recreate --no-deps controld
docker compose "${compose_args[@]}" up -d --force-recreate --no-deps tunneld node gatewayd
docker compose "${compose_args[@]}" up -d --force-recreate rollout-worker
bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

mock_ready=false
for _ in $(seq 1 60); do
  if docker compose "${compose_args[@]}" exec -T mock-provider wget -qO- --no-check-certificate https://127.0.0.1:24443/healthz >/dev/null 2>&1; then
    mock_ready=true
    break
  fi
  sleep 1
done
if [[ "${mock_ready}" != "true" ]]; then
  echo "managed rollout mock provider did not become ready" >&2
  exit 1
fi

docker compose "${compose_args[@]}" exec -T mock-provider \
  python /mock/mock_provider_contract.py --base-url "${mock_provider_base_url}" --ca /certs/ca.crt >/dev/null

docker compose "${compose_args[@]}" exec -T rollout-worker \
  axrun task publish /fixtures/bundle --target registry:5000/axrun/tasksets/managed-mock --publisher local >/dev/null
taskset_ref="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["descriptor_reference"])' "${fixture}/bundle/published.json")"

bundle_image="${CODEX_BUNDLE_IMAGE}"
if ! docker image inspect "${bundle_image}" >/dev/null 2>&1; then
  IMAGE_REF="${bundle_image}" \
    bash "${AXERN_ROOT}/runtime/axnoded/scripts/runtime/build-codex-bundle-image.sh" >/dev/null
fi
image_id="$(docker image inspect "${bundle_image}" --format '{{.Id}}')"
agent_ref="axern/codex-bundle@${image_id}"
node_container="${COMPOSE_PROJECT_NAME}-node-1"
archive="${fixture}/codex-bundle.tar"
docker save -o "${archive}" "${bundle_image}"
docker exec -i "${node_container}" /bin/bash -lc 'cat > /tmp/managed-rollout-codex-bundle.tar' < "${archive}"
docker exec "${node_container}" axctl --timeout 5m image import \
  --imagemgr-socket /run/imagemgr/imagemgr.sock \
  --archive /tmp/managed-rollout-codex-bundle.tar --ref "${agent_ref}" >/dev/null
docker exec "${node_container}" rm -f /tmp/managed-rollout-codex-bundle.tar

claude_bundle_image="${CLAUDE_CODE_BUNDLE_IMAGE}"
if ! docker image inspect "${claude_bundle_image}" >/dev/null 2>&1; then
  IMAGE_REF="${claude_bundle_image}" \
    bash "${AXERN_ROOT}/runtime/axnoded/scripts/runtime/build-claude-code-bundle-image.sh" >/dev/null
fi
claude_image_id="$(docker image inspect "${claude_bundle_image}" --format '{{.Id}}')"
claude_agent_ref="axern/claude-code-bundle@${claude_image_id}"
claude_archive="${fixture}/claude-code-bundle.tar"
docker save -o "${claude_archive}" "${claude_bundle_image}"
docker exec -i "${node_container}" /bin/bash -lc 'cat > /tmp/managed-rollout-claude-code-bundle.tar' < "${claude_archive}"
docker exec "${node_container}" axctl --timeout 5m image import \
  --imagemgr-socket /run/imagemgr/imagemgr.sock \
  --archive /tmp/managed-rollout-claude-code-bundle.tar --ref "${claude_agent_ref}" >/dev/null
docker exec "${node_container}" rm -f /tmp/managed-rollout-claude-code-bundle.tar

runtime_archive="${fixture}/mock-runtime.tar"
docker save -o "${runtime_archive}" "${mock_runtime_image}"
docker exec -i "${node_container}" /bin/bash -lc 'cat > /tmp/managed-rollout-mock-runtime.tar' < "${runtime_archive}"
docker exec "${node_container}" axctl --timeout 5m image import \
  --imagemgr-socket /run/imagemgr/imagemgr.sock \
  --archive /tmp/managed-rollout-mock-runtime.tar --ref "${mock_runtime_ref}" >/dev/null
docker exec "${node_container}" rm -f /tmp/managed-rollout-mock-runtime.tar

profile="managed-mock-codex-${run_suffix}"
printf '%s\n' mock-rotate-v1 | "${axrun}" --config "${config_file}" profile create "${profile}" \
  --agent codex --provider openai --wire-api responses --base-url "${mock_provider_base_url}/v1" \
  --max-concurrency 2 --token-stdin --idempotency-key "managed-mock-profile-${run_suffix}" >/dev/null
"${axrun}" --config "${config_file}" profile doctor "${profile}" --model mock-scripted-agent >/dev/null

rollout_file="${fixture}/rollout.yaml"
cat > "${rollout_file}" <<EOF
api_version: axrun/v1
kind: Rollout
metadata:
  name: managed-mock
spec:
  task_set:
    ref: ${taskset_ref}
  agent:
    name: codex
    runtime:
      kind: agent_image
      image: ${agent_ref}
    profile: ${profile}
    approval_policy: never
  model: mock-scripted-agent
  execution:
    runner: axern
    namespace: default
    runtime_class: runsc
    concurrency: 1
    attempts: 1
  budget:
    max_tokens: 1024
    max_cost_microusd: 10000
EOF

plan_output="$("${axrun}" --config "${config_file}" rollout plan --file "${rollout_file}")"
rollout_id="$(printf '%s\n' "${plan_output}" | awk -F'[ =]' '/^rollout=/{print $2}' | tail -1)"
test -n "${rollout_id}"

printf '%s\n' mock-rotate-v2 | "${axrun}" --config "${config_file}" profile rotate "${profile}" \
  --token-stdin --idempotency-key "managed-mock-rotate-${run_suffix}" >/dev/null
"${axrun}" --config "${config_file}" rollout start "${rollout_id}" >/dev/null

rollout_json="$("${axrun}" --config "${config_file}" --format json rollout get "${rollout_id}")"
python3 - "${rollout_json}" <<'PY'
import json
import sys

result = json.loads(sys.argv[1])
rollout = result["rollout"]
if rollout["status"] != "ROLLOUT_STATUS_COMPLETED":
    raise SystemExit("manual rollout did not complete")
if rollout["preflight"]["credentialVersion"] != "1":
    raise SystemExit("manual plan did not retain credential v1")
if int(rollout["preflight"]["usage"]["inputTokens"]) <= 0 or int(rollout["preflight"]["usage"]["costMicrousd"]) <= 0:
    raise SystemExit("preflight usage/cost was not metered")
episode = result["episodes"][0]
if not episode.get("passed") or float(episode.get("reward", 0)) <= 0:
    raise SystemExit("agent/verifier/reward did not pass")
PY

run_output="$("${axrun}" --config "${config_file}" rollout run --file "${rollout_file}")"
run_id="$(printf '%s\n' "${run_output}" | awk -F'[ =]' '/^rollout=/{print $2}' | tail -1)"
test -n "${run_id}"
run_json="$("${axrun}" --config "${config_file}" --format json rollout get "${run_id}")"
python3 - "${run_json}" <<'PY'
import json
import sys

rollout = json.loads(sys.argv[1])["rollout"]
if rollout["preflight"]["credentialVersion"] != "2":
    raise SystemExit("new rollout did not freeze credential v2")
PY

evidence_dir="${fixture}/evidence"
"${axrun}" --config "${config_file}" rollout artifact download-all "${run_id}" --output-dir "${evidence_dir}" >/dev/null
test "$(find "${evidence_dir}" -type f | wc -l | tr -d ' ')" -ge 6

anthropic_profile="managed-mock-anthropic-${run_suffix}"
printf '%s\n' mock-success | "${axrun}" --config "${config_file}" profile create "${anthropic_profile}" \
  --agent claude-code --provider anthropic --wire-api anthropic-messages \
  --base-url "${mock_provider_base_url}/v1" --token-stdin --idempotency-key "managed-mock-anthropic-${run_suffix}" >/dev/null
"${axrun}" --config "${config_file}" profile doctor "${anthropic_profile}" --model mock-scripted-agent >/dev/null

claude_rollout_file="${fixture}/claude-rollout.yaml"
cat > "${claude_rollout_file}" <<EOF
api_version: axrun/v1
kind: Rollout
metadata:
  name: managed-mock-claude
spec:
  task_set:
    ref: ${taskset_ref}
  agent:
    name: claude-code
    runtime:
      kind: agent_image
      image: ${claude_agent_ref}
    profile: ${anthropic_profile}
    approval_policy: never
  model: mock-scripted-agent
  execution:
    runner: axern
    namespace: default
    runtime_class: runsc
    concurrency: 1
    attempts: 1
  budget:
    max_tokens: 1024
    max_cost_microusd: 10000
EOF

set +e
claude_run_output="$("${axrun}" --config "${config_file}" rollout run --file "${claude_rollout_file}")"
claude_run_status=$?
set -e
claude_rollout_id="$(printf '%s\n' "${claude_run_output}" | awk -F'[ =]' '/^rollout=/{print $2}' | tail -1)"
test -n "${claude_rollout_id}"
if [[ "${claude_run_status}" -ne 0 ]]; then
  printf '%s\n' "${claude_run_output}" >&2
  exit "${claude_run_status}"
fi
claude_rollout_json="$("${axrun}" --config "${config_file}" --format json rollout get "${claude_rollout_id}")"
python3 - "${claude_rollout_json}" <<'PY'
import json
import sys

result = json.loads(sys.argv[1])
rollout = result["rollout"]
if rollout["status"] != "ROLLOUT_STATUS_COMPLETED":
    raise SystemExit("Claude Code rollout did not complete")
episode = result["episodes"][0]
if not episode.get("passed") or float(episode.get("reward", 0)) <= 0:
    raise SystemExit("Claude Code agent/verifier/reward did not pass")
PY

claude_evidence_dir="${fixture}/claude-evidence"
"${axrun}" --config "${config_file}" rollout artifact download-all "${claude_rollout_id}" \
  --output-dir "${claude_evidence_dir}" >/dev/null
test "$(find "${claude_evidence_dir}" -type f | wc -l | tr -d ' ')" -ge 6

request_log="$(docker compose "${compose_args[@]}" exec -T mock-provider wget -qO- --no-check-certificate https://127.0.0.1:24443/__mock/requests)"
python3 - "${request_log}" <<'PY'
import json
import sys

requests = json.loads(sys.argv[1])["requests"]
wires = {item["wire_api"] for item in requests}
versions = {item["credential_version"] for item in requests}
if wires != {"responses", "anthropic_messages"}:
    raise SystemExit("both provider wire APIs were not exercised")
if not {"v1", "v2"}.issubset(versions):
    raise SystemExit("credential snapshot versions were not exercised")
PY

echo "managed_rollout_compose_e2e_ok=true"
echo "manual_rollout_id=${rollout_id}"
echo "auto_rollout_id=${run_id}"
echo "claude_rollout_id=${claude_rollout_id}"
