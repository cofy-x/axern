#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock compose
smoke_root="$(mktemp -d)"
cancelled_run_id=""
cleanup() {
  if [ -n "${cancelled_run_id}" ]; then
    "${AXERN_SMOKE_CMD[@]}" run cancel "${cancelled_run_id}" -o json >/dev/null 2>&1 || true
  fi
  rm -rf "${smoke_root}"
  end_env_lock compose
}
trap cleanup EXIT

LOCAL_SMOKE_AXERN_TIMEOUT=10m local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
image_ref="${LOCAL_SMOKE_RESOLVED_IMAGE_REF:-}"
if [ -z "${image_ref}" ]; then
  image_ref="$(local_smoke_prepare_compose_image)"
fi

snapshot_stdout="${smoke_root}/snapshot.stdout"
snapshot_stderr="${smoke_root}/snapshot.stderr"
"${AXERN_SMOKE_CMD[@]}" run --snapshot-rootfs --request-cpu 100m --request-memory 512MiB "${image_ref}" -- \
  python -c 'import hashlib, pathlib, urllib.parse; stale=pathlib.Path("/etc/debian_version"); assert stale.exists(); stale.unlink(); p=pathlib.Path("/opt/axern-snapshot-smoke.bin"); p.write_bytes(b"immutable-rootfs-result\n"); p.chmod(0o751); print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${snapshot_stdout}" 2>"${snapshot_stderr}"

snapshot_environment="$(sed -n 's/^Snapshot Environment: //p' "${snapshot_stderr}" | tail -n 1)"
if [ -z "${snapshot_environment}" ]; then
  echo "snapshot Run did not return a derived Environment" >&2
  cat "${snapshot_stderr}" >&2
  exit 1
fi
expected_hash="$(tr -d '\r\n' <"${snapshot_stdout}")"

"${AXERN_SMOKE_CMD[@]}" run --request-cpu 100m --request-memory 512MiB --environment "${snapshot_environment}" -- \
  python -c 'import hashlib, pathlib; assert not pathlib.Path("/etc/debian_version").exists(); assert not pathlib.Path("/opt/fresh-run-one").exists(); p=pathlib.Path("/opt/axern-snapshot-smoke.bin"); assert oct(p.stat().st_mode & 0o777)=="0o751"; pathlib.Path("/opt/fresh-run-one").write_text("one"); print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${smoke_root}/derived-one.stdout" 2>"${smoke_root}/derived-one.stderr"
grep -Fxq "${expected_hash}" "${smoke_root}/derived-one.stdout"

"${AXERN_SMOKE_CMD[@]}" run --request-cpu 100m --request-memory 512MiB --environment "${snapshot_environment}" -- \
  python -c 'import hashlib, pathlib; assert not pathlib.Path("/etc/debian_version").exists(); assert not pathlib.Path("/opt/fresh-run-one").exists(); p=pathlib.Path("/opt/axern-snapshot-smoke.bin"); assert oct(p.stat().st_mode & 0o777)=="0o751"; pathlib.Path("/opt/fresh-run-two").write_text("two"); print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${smoke_root}/derived-two.stdout" 2>"${smoke_root}/derived-two.stderr"
grep -Fxq "${expected_hash}" "${smoke_root}/derived-two.stdout"

failed_stderr="${smoke_root}/failed.stderr"
if "${AXERN_SMOKE_CMD[@]}" run --snapshot-rootfs --request-cpu 100m --request-memory 512MiB "${image_ref}" -- \
  python -c 'raise SystemExit(17)' >"${smoke_root}/failed.stdout" 2>"${failed_stderr}"; then
  echo "failed workload unexpectedly returned success" >&2
  exit 1
fi
failed_run_id="$(sed -n 's/^Run: //p' "${failed_stderr}" | head -n 1)"
if [ -z "${failed_run_id}" ] || grep -q '^Snapshot Environment: ' "${failed_stderr}"; then
  echo "failed workload returned an invalid snapshot result" >&2
  cat "${failed_stderr}" >&2
  exit 1
fi
failed_json="$(local_smoke_wait_for_run_status "${failed_run_id}" failed)"
python3 -c 'import json,sys; run=json.load(sys.stdin)["run"]; snapshot=run.get("rootfs_snapshot", {}); assert run["status"] == "failed" and snapshot.get("status") != "ROOTFS_SNAPSHOT_STATUS_READY" and not snapshot.get("environment_id")' <<<"${failed_json}"

cancelled_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run --detach -o json --snapshot-rootfs --request-cpu 100m --request-memory 512MiB "${image_ref}" -- python -c 'import time; time.sleep(60)')"
cancelled_run_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"${cancelled_json}")"
local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run cancel "${cancelled_run_id}" -o json >/dev/null
cancelled_json="$(local_smoke_wait_for_run_status "${cancelled_run_id}" cancelled)"
python3 -c 'import json,sys; run=json.load(sys.stdin)["run"]; snapshot=run.get("rootfs_snapshot", {}); assert run["status"] == "cancelled" and snapshot.get("status") != "ROOTFS_SNAPSHOT_STATUS_READY" and not snapshot.get("environment_id")' <<<"${cancelled_json}"
cancelled_run_id=""

echo "compose_rootfs_snapshot_smoke_ok=true"
