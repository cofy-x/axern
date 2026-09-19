#!/usr/bin/env bash
set -euo pipefail

cli="${AXERN_CLI_BINARY:?AXERN_CLI_BINARY is required}"
smoke_root="$(mktemp -d)"
cleanup() {
  AXERN_HOME="${smoke_root}" "${cli}" local down >/dev/null 2>&1 || true
  AXERN_HOME="${smoke_root}" "${cli}" local reset --force >/dev/null 2>&1 || true
}

diagnostics() {
  echo "local release smoke failed; collecting diagnostics" >&2
  for output in run.stdout run.stderr snapshot.stdout snapshot.stderr branch-a.stdout branch-a.stderr branch-b.stdout branch-b.stderr exit.stdout exit.stderr; do
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
"${cli}" "${common[@]}" local reset --force
