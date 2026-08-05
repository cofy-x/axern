# Image Mounts

Image mounts are an Axern runtime composition primitive. A workload keeps its
task image as the sandbox rootfs while one or more reusable image bundles are
mounted read-only at explicit paths inside the same sandbox.

```text
task image rootfs
  + host/volume mounts
  + optional COW TaskSet workspace image
  + read-only image bundle mounts
  -> one sandbox process environment
```

This is generic platform behavior. Agent CLIs, compilers, debuggers, test
tools, and other reusable tool bundles use the same contract.

## Contract

`ExecutionConfig.image_mounts[]` carries:

- `image`: image reference resolved by the node image runtime.
- `target`: absolute sandbox path below `/`.
- `readonly`: read-only flag. Image mounts are read-only.

Validation rules:

- targets must not be `/` or protected system paths such as `/usr`, `/bin`,
  `/etc`, `/proc`, `/sys`, `/dev`, or `/run`;
- image mount targets must not overlap each other or existing sandbox, volume,
  template, sandboxd-managed, or secret-file mounts;
- missing directory targets are materialized in the final task rootfs view;
- paths that cross rootfs symlinks are rejected;
- setup failure fails allocation start, and allocation cleanup releases mounted
  image rootfs references.

Axern does not merge operating system roots. A mounted image contributes files
only. Tool images should be relocatable bundles, for example:

```text
/opt/axern/agents/codex/
  bin/
  lib/
```

Axern does not merge operating-system ABIs. A generic tool bundle that relies
on task libraries must document that dependency. Official Axern Claude Code and
Codex bundles instead carry a complete, pinned Ubuntu ABI and invoke their own
loader, so they support both glibc- and musl-based Axrun-compatible task images.
The task image remains responsible for `/bin/sh`, project commands, and its own
language and test toolchains.

## Runtime Ownership

- `controld` persists and propagates normalized image mount specs.
- `axnoded` validates targets, persists live-container image ownership, restores
  it after restart, and reconciles its complete desired lease set.
- `imagemgr` owns durable mount leases, image rootfs resolution, final release
  retry, and mounted rootfs lifetime.
- `imagefsd` remains the read-only image data plane for formats that need it.

The OCI mount shape is:

```text
type=bind
source=<resolved image rootfs>
target=<target>
options=["rbind","ro"]
```

Stable runtime IDs include image, target, and read-only flag so different mount
sets do not reuse the wrong runtime template.

## Axrun Use

Axrun uses image mounts for packaged agent tools:

```text
native task sandbox image
  + agent/tool image bundle mount
  + agent command inside the task sandbox
  -> patch, stdout, trajectory, raw evidence, exports
```

Task images and agent bundle images remain separate. Credentials and provider
endpoints stay in profile-backed managed proxy or local config, not in task
images or bundle images.

TaskSet workspaces are also separate from image mounts. They use the dedicated
`ExecutionConfig.workspace_image` contract and are prepared as writable,
allocation-local COW overlays. See [Workspace Images](workspace-images.md).

## Verification

- `make local-compose-image-mount-smoke`
- `make local-compose-agent-bundle-matrix-smoke`
- `make local-compose-claude-code-image-mount-smoke`
- `make local-compose-codex-image-mount-smoke`
- `make axrun-local-smoke` for the local functional path
- Linux Axern acceptance for workspace variant selection and COW isolation
