#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock compose
smoke_root="$(mktemp -d)"
cleanup() {
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
  python -c 'import hashlib, pathlib, sys; assert not pathlib.Path("/etc/debian_version").exists(); p=pathlib.Path("/opt/axern-snapshot-smoke.bin"); assert oct(p.stat().st_mode & 0o777)=="0o751"; print(hashlib.sha256(p.read_bytes()).hexdigest())' \
  >"${smoke_root}/derived.stdout" 2>"${smoke_root}/derived.stderr"
grep -Fxq "${expected_hash}" "${smoke_root}/derived.stdout"

echo "compose_rootfs_snapshot_smoke_ok=true"
