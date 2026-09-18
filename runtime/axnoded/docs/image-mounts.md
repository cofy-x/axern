# Image Mounts

Image mounts are an Axern runtime composition primitive. A workload keeps its task image as the sandbox rootfs while one or more reusable image bundles are mounted read-only at explicit paths inside the same sandbox.

```text
task image rootfs
  + runtime-owned bind mounts
  + read-only image mounts
  -> one sandbox process environment
```

This is generic platform behavior. Agent CLIs, compilers, debuggers, test tools, and other reusable tool bundles use the same contract.

## Contract

`ExecutionConfig.image_mounts[]` carries:

- `image`: image reference resolved by the node image runtime.
- `target`: absolute sandbox path below `/`.

Image mounts are unconditionally read-only. The public contract has no writable flag; controld resolves every accepted entry to an internal read-only runtime projection.

Validation rules:

- targets must not be `/` or protected system paths such as `/usr`, `/bin`, `/etc`, `/proc`, `/sys`, `/dev`, or `/run`;
- image mount targets must not overlap each other or existing sandbox, template, sandboxd-managed, or secret-file mounts;
- missing directory targets are materialized in the final task rootfs view;
- paths that cross rootfs symlinks are rejected;
- setup failure fails allocation start, and allocation cleanup releases mounted image rootfs references.

Axern does not merge operating system roots. A mounted image contributes files only. Tool images should be relocatable bundles, for example:

```text
/opt/axern/agents/codex/
  bin/
  lib/
```

Axern does not merge operating-system ABIs. A tool image that relies on task libraries must document that dependency. The task image remains responsible for `/bin/sh`, project commands, and its own language and test toolchains. Callers own tool-image construction and compatibility.

## Runtime Ownership

- `controld` persists and propagates normalized image mount specs.
- `axnoded` validates targets, persists live-container image ownership, restores it after restart, and reconciles its complete desired lease set.
- `imagemgr` owns durable mount leases, image rootfs resolution, final release retry, and mounted rootfs lifetime.
- `imagefsd` remains the read-only image data plane for formats that need it.

The OCI mount shape is:

```text
type=bind
source=<resolved image rootfs>
target=<target>
options=["rbind","ro"]
```

Stable runtime IDs include image, target, and the internal read-only invariant so different mount sets do not reuse the wrong environment template.

## SDK Consumer Use

An SDK consumer can use the generic image-mount primitive for caller-supplied read-only tools:

```text
base sandbox image
  + tool image mount
  + command inside the sandbox
  -> stdout and declared outputs
```

Base images and tool images remain separate. Axern receives only immutable image mounts and Allocation-local file/archive operations. Workload credentials that must exist inside the sandbox use explicit Run Secret projections; caller-local services remain reachable through an Allocation-scoped Tunnel without becoming image or Run metadata.

## Verification

- `make local-compose-image-mount-smoke` (read-only image mount plus environment/file Secret projections)
- Linux Axern acceptance for read-only mount isolation and restart recovery
