#!/bin/sh
set -eux

claude_code_version="$1"
target_arch="$2"
ubuntu_base_image="$3"
canonical_root=/__claude_code
canonical_loader="$canonical_root/l"

case "$target_arch" in
  amd64)
    loader=/lib64/ld-linux-x86-64.so.2
    multiarch=x86_64-linux-gnu
    elf_machine='Advanced Micro Devices X86-64'
    lib_alias=glibc0
    usr_lib_alias=glibc00000
    ;;
  arm64)
    loader=/lib/ld-linux-aarch64.so.1
    multiarch=aarch64-linux-gnu
    elf_machine=AArch64
    lib_alias=glibc00
    usr_lib_alias=glibc000000
    ;;
  *)
    echo "unsupported Claude Code bundle architecture: $target_arch" >&2
    exit 1
    ;;
esac

install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o /etc/apt/keyrings/claude-code.asc
fingerprint="$(gpg --show-keys --with-colons /etc/apt/keyrings/claude-code.asc | awk -F: '$1 == "fpr" {print $10; exit}')"
test "$fingerprint" = 31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE
echo "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/stable stable main" \
  > /etc/apt/sources.list.d/claude-code.list
apt-get update
apt-get install -y --no-install-recommends "claude-code=${claude_code_version}-1"
test "$(claude --version)" = "$claude_code_version (Claude Code)"

test -x "$loader"
install -d -m 0755 /opt/axern/agent-bundle/bin
install -m 0755 /usr/bin/claude /opt/axern/agent-bundle/bin/claude.real
file /opt/axern/agent-bundle/bin/claude.real | grep -Fq ELF
machine="$(readelf -h /opt/axern/agent-bundle/bin/claude.real | sed -n 's/^[[:space:]]*Machine:[[:space:]]*//p')"
test "$machine" = "$elf_machine"
test "$(patchelf --print-interpreter /opt/axern/agent-bundle/bin/claude.real)" = "$loader"
needed="$(patchelf --print-needed /opt/axern/agent-bundle/bin/claude.real)"
test -n "$needed"
ldd_output="$(ldd /opt/axern/agent-bundle/bin/claude.real)"
if printf '%s\n' "$ldd_output" | grep -Fq 'not found'; then
  echo "Claude Code has unresolved shared-library dependencies" >&2
  exit 1
fi

size_before="$(stat -c %s /opt/axern/agent-bundle/bin/claude.real)"
/usr/local/bin/patch-claude-bundle-elf bun /opt/axern/agent-bundle/bin/claude.real
test "$(stat -c %s /opt/axern/agent-bundle/bin/claude.real)" = "$size_before"
test "$(patchelf --print-interpreter /opt/axern/agent-bundle/bin/claude.real)" = "$canonical_loader"

cp --dereference "$loader" /tmp/claude-loader
/usr/local/bin/patch-claude-bundle-elf loader /tmp/claude-loader "$multiarch"
chmod 0755 /tmp/claude-loader

ln -s / "$canonical_root"
ln -s "lib/$multiarch" "/$lib_alias"
ln -s "usr/lib/$multiarch" "/$usr_lib_alias"
cp /tmp/claude-loader /l
test "$(env -u LD_LIBRARY_PATH /opt/axern/agent-bundle/bin/claude.real --version)" = "$claude_code_version (Claude Code)"
loader_trace="$(env -u LD_LIBRARY_PATH "$canonical_loader" --list /opt/axern/agent-bundle/bin/claude.real)"
printf '%s\n' "$loader_trace" | awk -v root="$canonical_root" '
  {
    for (i = 1; i <= NF; i++)
      if (substr($i, 1, 1) == "/" && (i == NF || $(i + 1) != "=>") && index($i, root) != 1)
        exit 1
  }
'
rm "$canonical_root" "/$lib_alias" "/$usr_lib_alias" /l

jq -n \
  --arg schema_version "2" \
  --arg agent_id "claude-code" \
  --arg agent_version "$claude_code_version" \
  --arg architecture "linux/$target_arch" \
  --arg ubuntu_base_image "$ubuntu_base_image" \
  --arg canonical_mount_target "$canonical_root" \
  --arg public_mount_target "/opt/axern/agents/claude-code" \
  --arg entrypoint "/bin/claude" \
  --arg loader "/l" \
  --arg ca_bundle "/etc/ssl/certs/ca-certificates.crt" \
  --arg multiarch "$multiarch" \
  '{schema_version: $schema_version, agent_id: $agent_id, agent_version: $agent_version, architecture: $architecture, ubuntu_base_image: $ubuntu_base_image, canonical_mount_target: $canonical_mount_target, public_mount_target: $public_mount_target, entrypoint: $entrypoint, loader: $loader, ca_bundle: $ca_bundle, multiarch: $multiarch, node_version: null, elfs: ["/opt/axern/agent-bundle/bin/claude.real"], dynamic_executables: ["/opt/axern/agent-bundle/bin/claude.real"], shared_objects: [], static_elfs: [], loader_wrapped_elfs: [], in_place_elfs: ["/opt/axern/agent-bundle/bin/claude.real"]}' \
  > /opt/axern/agent-bundle/manifest.json
