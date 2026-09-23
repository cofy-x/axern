#!/usr/bin/env bash
set -euo pipefail

cli="${AXERN_CLI_BINARY:?AXERN_CLI_BINARY is required}"
smoke_root="$(mktemp -d)"
external_registry_container=""
external_registry_tag=""
cleanup() {
  if [[ -n "${external_registry_container}" ]]; then
    docker rm --force "${external_registry_container}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${external_registry_tag}" ]]; then
    docker image rm "${external_registry_tag}" >/dev/null 2>&1 || true
  fi
  AXERN_HOME="${smoke_root}" "${cli}" local down >/dev/null 2>&1 || true
  AXERN_HOME="${smoke_root}" "${cli}" local reset --force >/dev/null 2>&1 || true
}

diagnostics() {
  echo "local release smoke failed; collecting diagnostics" >&2
  for output in run.stdout run.stderr snapshot.stdout snapshot.stderr branch-a.stdout branch-a.stderr branch-b.stdout branch-b.stderr exit.stdout exit.stderr dns-doctor.json external-push.log external-tag-environment.json external-environment.json external-doctor.json external-run.stdout external-run.stderr; do
    if [[ -s "${smoke_root}/${output}" ]]; then
      echo "--- ${output} ---" >&2
      sed -n '1,240p' "${smoke_root}/${output}" >&2
    fi
  done
  echo "--- local status ---" >&2
  AXERN_HOME="${smoke_root}" "${cli}" --config "${smoke_root}/config.json" --timeout 30s local status >&2 || true
  echo "--- local capability snapshot ---" >&2
  curl --fail --silent --show-error --max-time 10 http://127.0.0.1:24101/nodesz >&2 || true
  echo >&2
  node_container="$(docker ps \
    --filter label=com.docker.compose.project=axern-local \
    --filter label=com.docker.compose.service=node \
    --format '{{.ID}}' | head -n 1)"
  if [[ -n "${node_container}" ]]; then
    echo "--- axnoded log ---" >&2
    docker exec "${node_container}" sh -c 'tail -n 500 /var/log/axnoded/axnoded.log' >&2 || true
  fi
  echo "--- local logs ---" >&2
  AXERN_HOME="${smoke_root}" "${cli}" --config "${smoke_root}/config.json" --timeout 30s local logs --tail 200 >&2 || true
}

on_exit() {
  status=$?
  if [[ "${status}" -ne 0 ]]; then
    diagnostics
  fi
  cleanup
  return "${status}"
}
trap on_exit EXIT

export AXERN_HOME="${smoke_root}"
config="${smoke_root}/config.json"
common=(--config "${config}" --timeout 10m)

"${cli}" "${common[@]}" local up --use
"${cli}" "${common[@]}" local status --output json | grep -q '"state": "running"'
"${cli}" "${common[@]}" local image load python:3.12-slim --pull
"${cli}" "${common[@]}" local doctor --probe --image python:3.12-slim --output json >"${smoke_root}/dns-doctor.json"
python3 - "${smoke_root}/dns-doctor.json" <<'PY'
import json
import sys

report = json.load(open(sys.argv[1], encoding="utf-8"))
if report.get("status") != "healthy" or report.get("mode") != "probe":
    raise SystemExit("source-free DNS probe was not healthy")
checks = {check["name"]: check for check in report["checks"]}
expected = {
    "runtime_dns_config": "runtime_dns_config_valid",
    "runtime_dns_node": "runtime_dns_node_reachable",
    "runtime_dns_sandbox": "runtime_dns_sandbox_resolved",
}
for name, code in expected.items():
    check = checks.get(name, {})
    if check.get("status") != "pass" or check.get("code") != code:
        raise SystemExit(f"source-free DNS probe failed: {name}")
PY

internal_registry_container="$(docker ps \
  --filter label=com.docker.compose.project=axern-local \
  --filter label=com.docker.compose.service=registry \
  --format '{{.ID}}' | head -n 1)"
if [[ -z "${internal_registry_container}" ]]; then
  echo "local release smoke could not find the internal registry container" >&2
  exit 1
fi
registry_image_id="$(docker inspect --format '{{.Image}}' "${internal_registry_container}")"
external_registry_container="axern-local-registry-smoke-$$"
docker run --detach --name "${external_registry_container}" \
  --network axern-local-registry --network-alias forge-seed-registry \
  --publish 127.0.0.1::5000 "${registry_image_id}" >/dev/null
external_registry_port="$(docker port "${external_registry_container}" 5000/tcp | awk -F: 'NR == 1 {print $NF}')"
if [[ -z "${external_registry_port}" ]]; then
  echo "external registry did not publish a host port" >&2
  exit 1
fi
for _ in $(seq 1 30); do
  if curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${external_registry_port}/v2/" >/dev/null; then
    break
  fi
  sleep 1
done
curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:${external_registry_port}/v2/" >/dev/null

external_registry_tag="127.0.0.1:${external_registry_port}/axern/external-registry-smoke:dev"
docker tag python:3.12-slim "${external_registry_tag}"
docker push "${external_registry_tag}" >"${smoke_root}/external-push.log" 2>&1
external_registry_digest="$(sed -n 's/^.*digest: \(sha256:[0-9a-f]\{64\}\).*$/\1/p' "${smoke_root}/external-push.log" | tail -n 1)"
if [[ -z "${external_registry_digest}" ]]; then
  echo "external registry push did not report a manifest digest" >&2
  exit 1
fi
external_image_ref="forge-seed-registry:5000/axern/external-registry-smoke@${external_registry_digest}"
external_tag_ref="forge-seed-registry:5000/axern/external-registry-smoke:dev"

"${cli}" "${common[@]}" local up --use --insecure-registry forge-seed-registry:5000
"${cli}" "${common[@]}" local status --output json | python3 -c 'import json,sys; value=json.load(sys.stdin); assert value["registry_network"] == "axern-local-registry"; assert value["external_insecure_registries"] == ["forge-seed-registry:5000"]'
set +e
"${cli}" "${common[@]}" local doctor --output json >"${smoke_root}/external-doctor.json"
doctor_status=$?
set -e
if [[ "${doctor_status}" -ne 0 && "${doctor_status}" -ne 1 ]]; then
  echo "local doctor failed while checking the external registry contract" >&2
  exit 1
fi
python3 - "${smoke_root}/external-doctor.json" <<'PY'
import json
import sys

report = json.load(open(sys.argv[1], encoding="utf-8"))
checks = {check["name"]: check for check in report["checks"]}
assert checks["registry_policy"]["code"] == "registry_policy_consistent"
assert checks["registry_network"]["code"] == "registry_network_available"
PY

"${cli}" "${common[@]}" environment create --image-ref "${external_tag_ref}" --output json >"${smoke_root}/external-tag-environment.json"
external_tag_environment_id="$(python3 - "${smoke_root}/external-tag-environment.json" "${external_tag_ref}" "${external_registry_digest}" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))["environment"]
assert value["spec"]["image"]["ref"] == sys.argv[2]
assert value["resolved_spec"]["image_descriptor"]["digest"] == sys.argv[3]
print(value["id"])
PY
)"
"${cli}" "${common[@]}" environment delete "${external_tag_environment_id}" >/dev/null

"${cli}" "${common[@]}" environment create --image-ref "${external_image_ref}" --output json >"${smoke_root}/external-environment.json"
external_environment_id="$(python3 - "${smoke_root}/external-environment.json" "${external_image_ref}" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))["environment"]
assert value["spec"]["image"]["ref"] == sys.argv[2]
assert value["resolved_spec"]["image_descriptor"]["digest"] == sys.argv[2].rsplit("@", 1)[1]
print(value["id"])
PY
)"
"${cli}" "${common[@]}" environment get "${external_environment_id}" --output json >"${smoke_root}/external-environment.json"
python3 - "${smoke_root}/external-environment.json" "${external_image_ref}" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))["environment"]
assert value["spec"]["image"]["ref"] == sys.argv[2]
assert value["resolved_spec"]["image_descriptor"]["digest"] == sys.argv[2].rsplit("@", 1)[1]
PY
"${cli}" "${common[@]}" run --request-cpu 100m --request-memory 512MiB --environment "${external_environment_id}" -- \
  python -c 'print("hello from external registry")' \
  >"${smoke_root}/external-run.stdout" 2>"${smoke_root}/external-run.stderr"
grep -Fxq "hello from external registry" "${smoke_root}/external-run.stdout"
"${cli}" "${common[@]}" environment delete "${external_environment_id}" >/dev/null

stdout_file="${smoke_root}/run.stdout"
stderr_file="${smoke_root}/run.stderr"
"${cli}" "${common[@]}" run --request-cpu 100m --request-memory 512MiB python:3.12-slim -- \
  python -c 'import sys; print("hello from axern"); print("hello from stderr", file=sys.stderr)' \
  >"${stdout_file}" 2>"${stderr_file}"
grep -Fxq "hello from axern" "${stdout_file}"
grep -Fq "hello from stderr" "${stderr_file}"

snapshot_stdout="${smoke_root}/snapshot.stdout"
snapshot_stderr="${smoke_root}/snapshot.stderr"
"${cli}" "${common[@]}" run --snapshot-rootfs --request-cpu 100m --request-memory 512MiB python:3.12-slim -- \
  python -c 'import hashlib, pathlib; stale=pathlib.Path("/etc/debian_version"); assert stale.exists(); stale.unlink(); p=pathlib.Path("/opt/axern-compiled.bin"); p.write_bytes(b"immutable-rootfs-result\n"); p.chmod(0o751); print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${snapshot_stdout}" 2>"${snapshot_stderr}"
snapshot_environment="$(sed -n 's/^Snapshot Environment: //p' "${snapshot_stderr}" | tail -n 1)"
if [[ -z "${snapshot_environment}" ]]; then
  echo "snapshot Run did not return a derived Environment" >&2
  exit 1
fi
expected_hash="$(tr -d '\r\n' <"${snapshot_stdout}")"

"${cli}" "${common[@]}" run --request-cpu 100m --request-memory 512MiB --environment "${snapshot_environment}" -- \
  python -c 'import hashlib, pathlib; assert not pathlib.Path("/etc/debian_version").exists(); p=pathlib.Path("/opt/axern-compiled.bin"); assert oct(p.stat().st_mode & 0o777)=="0o751"; assert p.stat().st_uid==0 and p.stat().st_gid==0; print(hashlib.sha256(p.read_bytes()).hexdigest()); pathlib.Path("/opt/branch-a").write_text("private\n")' \
  >"${smoke_root}/branch-a.stdout" 2>"${smoke_root}/branch-a.stderr"
grep -Fxq "${expected_hash}" "${smoke_root}/branch-a.stdout"

"${cli}" "${common[@]}" run --request-cpu 100m --request-memory 512MiB --environment "${snapshot_environment}" -- \
  python -c 'import hashlib, pathlib; assert not pathlib.Path("/etc/debian_version").exists(); p=pathlib.Path("/opt/axern-compiled.bin"); assert not pathlib.Path("/opt/branch-a").exists(); print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${smoke_root}/branch-b.stdout" 2>"${smoke_root}/branch-b.stderr"
grep -Fxq "${expected_hash}" "${smoke_root}/branch-b.stdout"

set +e
"${cli}" "${common[@]}" run --request-cpu 100m --request-memory 512MiB python:3.12-slim -- python -c 'raise SystemExit(7)' \
  >"${smoke_root}/exit.stdout" 2>"${smoke_root}/exit.stderr"
run_status=$?
set -e
if [[ "${run_status}" -ne 7 ]]; then
  echo "foreground Run returned ${run_status}, want workload exit code 7" >&2
  exit 1
fi
"${cli}" "${common[@]}" local down
"${cli}" "${common[@]}" local up --use
"${cli}" "${common[@]}" local status --output json | python3 -c 'import json,sys; assert json.load(sys.stdin)["external_insecure_registries"] == ["forge-seed-registry:5000"]'
"${cli}" "${common[@]}" local reset --force
