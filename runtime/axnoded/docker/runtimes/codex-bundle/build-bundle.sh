#!/bin/sh
set -eux

codex_cli_version="$1"
node_version="$2"
target_arch="$3"
ubuntu_base_image="$4"
node_sha256_amd64="$5"
node_sha256_arm64="$6"
node_download_base_url="${7%/}"
npm_registry="$8"
canonical_root=/opt/axern/agents/codex

case "$target_arch" in
  amd64)
    node_arch=x64
    node_sha256="$node_sha256_amd64"
    loader=/lib64/ld-linux-x86-64.so.2
    multiarch=x86_64-linux-gnu
    elf_machine='Advanced Micro Devices X86-64'
    ;;
  arm64)
    node_arch=arm64
    node_sha256="$node_sha256_arm64"
    loader=/lib/ld-linux-aarch64.so.1
    multiarch=aarch64-linux-gnu
    elf_machine=AArch64
    ;;
  *)
    echo "unsupported Codex bundle architecture: $target_arch" >&2
    exit 1
    ;;
esac

test -x "$loader"
archive="node-v${node_version}-linux-${node_arch}.tar.xz"
curl -fsSLo "/tmp/$archive" "$node_download_base_url/v${node_version}/$archive"
echo "$node_sha256  /tmp/$archive" | sha256sum -c -
install -d -m 0755 /tmp/node
tar -xJf "/tmp/$archive" --strip-components=1 -C /tmp/node
rm -f "/tmp/$archive"
PATH="/tmp/node/bin:$PATH" npm install --global --prefix /opt/axern/agent-bundle/npm \
  --registry "$npm_registry" "@openai/codex@$codex_cli_version"
package_dir=/opt/axern/agent-bundle/npm/lib/node_modules/@openai/codex
installed_version="$(/tmp/node/bin/node -p "require('$package_dir/package.json').version")"
test "$installed_version" = "$codex_cli_version"
/tmp/node/bin/node "$package_dir/bin/codex.js" --version | grep -Fx "codex-cli $codex_cli_version"
install -d -m 0755 /opt/axern/agent-bundle/node/bin
install -m 0755 /tmp/node/bin/node /opt/axern/agent-bundle/node/bin/node
rm -rf /tmp/node

: > /tmp/elfs
: > /tmp/dynamic
: > /tmp/shared
: > /tmp/static
: > /tmp/wrapped
find /opt/axern/agent-bundle -type f -print | while IFS= read -r candidate; do
  file -b "$candidate" | grep -q '^ELF' || continue
  relative="/${candidate#/}"
  echo "$relative" >> /tmp/elfs
  machine="$(readelf -h "$candidate" | sed -n 's/^[[:space:]]*Machine:[[:space:]]*//p')"
  test "$machine" = "$elf_machine"
  dynamic_tags="$(readelf -d "$candidate")"
  has_dynamic=false
  needed=""
  if ! printf '%s\n' "$dynamic_tags" | grep -Fq 'There is no dynamic section'; then
    has_dynamic=true
    needed="$(patchelf --print-needed "$candidate")"
  fi
  if [ "$candidate" = /opt/axern/agent-bundle/node/bin/node ]; then
    [ "$has_dynamic" = true ]
    test "$(patchelf --print-interpreter "$candidate")" = "$loader"
    test -n "$needed"
    echo "$relative" >> /tmp/wrapped
    ldd_output="$(ldd "$candidate")"
    if printf '%s\n' "$ldd_output" | grep -Fq 'not found'; then
      echo "Codex Node has unresolved shared-library dependencies" >&2
      exit 1
    fi
    continue
  fi
  if readelf -l "$candidate" | grep -Fq 'Requesting program interpreter'; then
    [ "$has_dynamic" = true ]
    test -n "$(patchelf --print-interpreter "$candidate")"
    test -n "$needed"
    ldd_output="$(ldd "$candidate")"
    if printf '%s\n' "$ldd_output" | grep -Fq 'not found'; then
      echo "Codex ELF has unresolved dependencies: $relative" >&2
      exit 1
    fi
    old_rpath="$(patchelf --print-rpath "$candidate")"
    canonical_rpath="$canonical_root/lib/$multiarch:$canonical_root/usr/lib/$multiarch:$canonical_root/lib:$canonical_root/usr/lib"
    if [ -n "$old_rpath" ]; then canonical_rpath="$old_rpath:$canonical_rpath"; fi
    patchelf --set-interpreter "$canonical_root$loader" --force-rpath --set-rpath "$canonical_rpath" "$candidate"
    test "$(patchelf --print-interpreter "$candidate")" = "$canonical_root$loader"
    echo "$relative" >> /tmp/dynamic
  elif [ "$has_dynamic" = true ] && [ -n "$needed" ]; then
    old_rpath="$(patchelf --print-rpath "$candidate")"
    canonical_rpath="$canonical_root/lib/$multiarch:$canonical_root/usr/lib/$multiarch:$canonical_root/lib:$canonical_root/usr/lib"
    if [ -n "$old_rpath" ]; then canonical_rpath="$old_rpath:$canonical_rpath"; fi
    patchelf --force-rpath --set-rpath "$canonical_rpath" "$candidate"
    echo "$relative" >> /tmp/shared
  else
    echo "$relative" >> /tmp/static
  fi
done

for classification in elfs dynamic shared static wrapped; do
  sort -u -o "/tmp/$classification" "/tmp/$classification"
done
test -s /tmp/elfs
test -s /tmp/wrapped
install -d -m 0755 /opt/axern/agents
ln -s / "$canonical_root"
library_path="$canonical_root/lib/$multiarch:$canonical_root/usr/lib/$multiarch:$canonical_root/lib:$canonical_root/usr/lib"
cat /tmp/dynamic /tmp/shared | while IFS= read -r elf; do
  [ -n "$elf" ] || continue
  ldd_output="$(ldd "$elf")"
  if printf '%s\n' "$ldd_output" | grep -Fq 'not found'; then
    echo "patched Codex ELF has unresolved dependencies: $elf" >&2
    exit 1
  fi
done
while IFS= read -r elf; do
  [ -n "$elf" ] || continue
  loader_trace="$("$canonical_root$loader" --library-path "$library_path" --list "$elf")"
  printf '%s\n' "$loader_trace" | awk -v root="$canonical_root" -v loader="$loader" '
    {
      for (i = 1; i <= NF; i++)
        if (substr($i, 1, 1) == "/" && (i == NF || $(i + 1) != "=>") && index($i, root) != 1 && $i != loader)
          exit 1
    }
  '
done < /tmp/wrapped
rm "$canonical_root"

jq -n \
  --arg schema_version "1" \
  --arg agent_id "codex" \
  --arg agent_version "$codex_cli_version" \
  --arg architecture "linux/$target_arch" \
  --arg ubuntu_base_image "$ubuntu_base_image" \
  --arg canonical_mount_target "$canonical_root" \
  --arg entrypoint "/bin/codex" \
  --arg loader "$loader" \
  --arg ca_bundle "/etc/ssl/certs/ca-certificates.crt" \
  --arg multiarch "$multiarch" \
  --arg node_version "$node_version" \
  --argjson elfs "$(jq -R -s 'split("\n") | map(select(length > 0))' /tmp/elfs)" \
  --argjson dynamic "$(jq -R -s 'split("\n") | map(select(length > 0))' /tmp/dynamic)" \
  --argjson shared "$(jq -R -s 'split("\n") | map(select(length > 0))' /tmp/shared)" \
  --argjson static "$(jq -R -s 'split("\n") | map(select(length > 0))' /tmp/static)" \
  --argjson wrapped "$(jq -R -s 'split("\n") | map(select(length > 0))' /tmp/wrapped)" \
  '{schema_version: $schema_version, agent_id: $agent_id, agent_version: $agent_version, architecture: $architecture, ubuntu_base_image: $ubuntu_base_image, canonical_mount_target: $canonical_mount_target, entrypoint: $entrypoint, loader: $loader, ca_bundle: $ca_bundle, multiarch: $multiarch, node_version: $node_version, elfs: $elfs, dynamic_executables: $dynamic, shared_objects: $shared, static_elfs: $static, loader_wrapped_elfs: $wrapped}' \
  > /opt/axern/agent-bundle/manifest.json
