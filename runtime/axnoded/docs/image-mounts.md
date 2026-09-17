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

## External Runner Use

An external runner can use the generic image-mount primitive for caller-supplied agent tools:

```text
native task sandbox image
  + agent/tool image mount
  + agent command inside the task sandbox
  -> patch, stdout, trajectory, raw evidence, exports
```

Task images and tool images remain separate. The external runner owns agent configuration, benchmark input, and task materialization; Axern receives only immutable image mounts and Allocation-local file/archive operations. Ordinary workload Secrets may use explicit Run projections. Model-provider identity remains in the external runner and is reached through an Allocation-scoped Tunnel to its loopback model gateway; it is neither baked into the image nor projected into the sandbox.

## Verification

- `make local-compose-image-mount-smoke` (read-only image mount plus environment/file Secret projections)
- Linux Axern acceptance for read-only mount isolation and restart recovery
