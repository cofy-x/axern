#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <image> <agent-id> <binary> <version> <version-output>" >&2
  exit 2
fi

image_ref="$1"
agent_id="$2"
binary="$3"
agent_version="$4"
version_output="$5"
mount_target="/opt/axern/agents/${agent_id}"
extract_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-agent-bundle.XXXXXX")"
negative_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-agent-bundle-negative.XXXXXX")"
container_name="axern-agent-bundle-verify-${agent_id}-$$"

cleanup() {
  docker rm -f "${container_name}" >/dev/null 2>&1 || true
  rm -rf "${extract_dir}"
  rm -rf "${negative_dir}"
}
trap cleanup EXIT

docker image inspect "${image_ref}" >/dev/null
image_arch="$(docker image inspect "${image_ref}" --format '{{.Architecture}}')"
case "${image_arch}" in
  amd64) loader_relative=lib64/ld-linux-x86-64.so.2; multiarch=x86_64-linux-gnu ;;
  arm64) loader_relative=lib/ld-linux-aarch64.so.1; multiarch=aarch64-linux-gnu ;;
  *) echo "unsupported bundle image architecture ${image_arch}" >&2; exit 1 ;;
esac
docker create --name "${container_name}" "${image_ref}" >/dev/null
docker export "${container_name}" | tar -xf - -C "${extract_dir}"

for base_image in ${AGENT_BUNDLE_VERIFY_BASE_IMAGES:-busybox:1.36 ubuntu:24.04}; do
  if ! docker image inspect "${base_image}" >/dev/null 2>&1; then
    docker pull "${base_image}" >/dev/null
  fi
  started_at="$(date +%s)"
  docker run --rm \
    --volume "${extract_dir}:${mount_target}:ro" \
    --env "BUNDLE_MOUNT_TARGET=${mount_target}" \
    --env "BUNDLE_BINARY=${binary}" \
    --env "BUNDLE_AGENT_ID=${agent_id}" \
    --env "BUNDLE_AGENT_VERSION=${agent_version}" \
    --env "BUNDLE_ARCHITECTURE=linux/${image_arch}" \
    --env "BUNDLE_LOADER_RELATIVE=${loader_relative}" \
    --env "BUNDLE_MULTIARCH=${multiarch}" \
    --env "BUNDLE_VERSION_OUTPUT=${version_output}" \
    --env "AXRUN_AGENT_BUNDLE_MOUNT_TARGET=${mount_target}" \
    "${base_image}" /bin/sh -lc '
      set -eu
      manifest="$BUNDLE_MOUNT_TARGET/opt/axern/agent-bundle/manifest.json"
      test -r "$manifest"
      grep -F "\"agent_id\": \"$BUNDLE_AGENT_ID\"" "$manifest"
      grep -F "\"agent_version\": \"$BUNDLE_AGENT_VERSION\"" "$manifest"
      grep -F "\"architecture\": \"$BUNDLE_ARCHITECTURE\"" "$manifest"
      grep -F "\"canonical_mount_target\": \"$BUNDLE_MOUNT_TARGET\"" "$manifest"
      LD_LIBRARY_PATH=/definitely-not-a-system-library \
        "$BUNDLE_MOUNT_TARGET/bin/$BUNDLE_BINARY" --version > /tmp/agent-version.txt
      grep -Fx "$BUNDLE_VERSION_OUTPUT" /tmp/agent-version.txt
      loader="$BUNDLE_MOUNT_TARGET/$BUNDLE_LOADER_RELATIVE"
      library_path="$BUNDLE_MOUNT_TARGET/lib/$BUNDLE_MULTIARCH:$BUNDLE_MOUNT_TARGET/usr/lib/$BUNDLE_MULTIARCH:$BUNDLE_MOUNT_TARGET/lib:$BUNDLE_MOUNT_TARGET/usr/lib"
      case "$BUNDLE_AGENT_ID" in
        claude-code) trace_binary="$BUNDLE_MOUNT_TARGET/opt/axern/agent-bundle/bin/claude.real" ;;
        codex) trace_binary="$BUNDLE_MOUNT_TARGET/opt/axern/agent-bundle/node/bin/node" ;;
        *) exit 1 ;;
      esac
      assert_bundle_trace() {
        awk -v root="$BUNDLE_MOUNT_TARGET" -v loader="/$BUNDLE_LOADER_RELATIVE" "{
          for (i = 1; i <= NF; i++)
            if (substr(\$i, 1, 1) == \"/\" && (i == NF || \$(i + 1) != \"=>\") && index(\$i, root) != 1 && \$i != loader)
              exit 1
        }"
      }
      loader_trace="$("$loader" --library-path "$library_path" --list "$trace_binary")"
      printf "%s\n" "$loader_trace" | assert_bundle_trace
      if [ "$BUNDLE_AGENT_ID" = codex ]; then
        rg_path="$(find "$BUNDLE_MOUNT_TARGET/opt/axern/agent-bundle" -type f -path "*/codex-path/rg" | head -n 1)"
        zsh_path="$(find "$BUNDLE_MOUNT_TARGET/opt/axern/agent-bundle" -type f -path "*/codex-resources/zsh/bin/zsh" | head -n 1)"
        test -n "$rg_path"
        test -n "$zsh_path"
        LD_TRACE_LOADED_OBJECTS=1 "$rg_path" | assert_bundle_trace
        LD_TRACE_LOADED_OBJECTS=1 "$zsh_path" | assert_bundle_trace
        "$rg_path" --version | grep -F "ripgrep"
        "$zsh_path" --version | grep -F "zsh"
      fi
      ! touch "$BUNDLE_MOUNT_TARGET/write-test"
      /bin/sh -c "printf sandbox-ok"
    '
  duration_seconds="$(( $(date +%s) - started_at ))"
  printf 'agent_bundle_case_ok=true agent=%s base_image=%s duration_seconds=%s\n' \
    "${agent_id}" "${base_image}" "${duration_seconds}"
done

wrong_target="/opt/axern/agents/noncanonical-${agent_id}"
if wrong_output="$(
  docker run --rm \
    --volume "${extract_dir}:${wrong_target}:ro" \
    --env "AXRUN_AGENT_BUNDLE_MOUNT_TARGET=${wrong_target}" \
    busybox:1.36 "${wrong_target}/bin/${binary}" --version 2>&1
)"; then
  echo "agent bundle unexpectedly accepted non-canonical mount target ${wrong_target}" >&2
  exit 1
fi
printf '%s\n' "${wrong_output}" | grep -F "${mount_target}" >/dev/null

for missing_relative in \
  opt/axern/agent-bundle/manifest.json \
  etc/ssl/certs/ca-certificates.crt \
  "${loader_relative}"; do
  case_dir="${negative_dir}/case-$(printf '%s' "${missing_relative}" | tr / -)"
  mkdir -p "${case_dir}"
  cp -al "${extract_dir}/." "${case_dir}/"
  rm "${case_dir}/${missing_relative}"
  set +e
  missing_output="$(
    docker run --rm \
      --volume "${case_dir}:${mount_target}:ro" \
      --env "AXRUN_AGENT_BUNDLE_MOUNT_TARGET=${mount_target}" \
      busybox:1.36 "${mount_target}/bin/${binary}" --version 2>&1
  )"
  missing_status=$?
  set -e
  if [ "${missing_status}" -eq 0 ]; then
    echo "agent bundle unexpectedly started with missing ${missing_relative}" >&2
    exit 1
  fi
  printf '%s\n' "${missing_output}" | grep -F "${mount_target}" >/dev/null
done

labels="$(docker image inspect "${image_ref}" --format '{{json .Config.Labels}}')"
printf '%s\n' "${labels}" | grep -F "\"io.axern.agent-bundle.agent-id\":\"${agent_id}\"" >/dev/null
printf '%s\n' "${labels}" | grep -F "\"io.axern.agent-bundle.agent-version\":\"${agent_version}\"" >/dev/null
printf '%s\n' "${labels}" | grep -F "\"io.axern.agent-bundle.architecture\":\"linux/${image_arch}\"" >/dev/null
printf '%s\n' "${labels}" | grep -F "\"io.axern.agent-bundle.mount-target\":\"${mount_target}\"" >/dev/null
printf '%s\n' "${labels}" | grep -F "\"io.axern.agent-bundle.entrypoint\":\"/bin/${binary}\"" >/dev/null
python3 - "${extract_dir}/opt/axern/agent-bundle/manifest.json" "${labels}" <<'PY'
import json
import pathlib
import sys

manifest = json.loads(pathlib.Path(sys.argv[1]).read_text())
labels = json.loads(sys.argv[2])
mapping = {
    "schema_version": "io.axern.agent-bundle.schema-version",
    "agent_id": "io.axern.agent-bundle.agent-id",
    "agent_version": "io.axern.agent-bundle.agent-version",
    "architecture": "io.axern.agent-bundle.architecture",
    "canonical_mount_target": "io.axern.agent-bundle.mount-target",
    "entrypoint": "io.axern.agent-bundle.entrypoint",
    "ubuntu_base_image": "io.axern.agent-bundle.ubuntu-base-image",
}
if "node_version" in manifest:
    mapping["node_version"] = "io.axern.agent-bundle.node-version"
for manifest_key, label_key in mapping.items():
    if manifest.get(manifest_key) != labels.get(label_key):
        raise SystemExit(
            f"manifest {manifest_key}={manifest.get(manifest_key)!r} does not match "
            f"OCI label {label_key}={labels.get(label_key)!r}"
        )
PY
image_size_bytes="$(docker image inspect "${image_ref}" --format '{{.Size}}')"

echo "agent_bundle_image_verified=true"
echo "agent_bundle_image=${image_ref}"
echo "agent_bundle_image_size_bytes=${image_size_bytes}"
