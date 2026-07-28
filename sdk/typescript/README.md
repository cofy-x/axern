# Axern TypeScript SDK

Node.js SDK for programmable Axern sandboxes.

This first SDK surface is intentionally focused on the programmable sandbox path:

- create or attach a `Sandbox`
- run `exec`
- start attached `process` streams
- use platform file RPCs such as `readFile`, `writeFile`, `stat`, and `listDir`
- transfer directories with archive-backed `uploadDir` and `downloadDir`
- expose local services to the sandbox with `tunnel`

## Install

From this repository workspace:

```bash
pnpm install
make sdk-typescript-verify
```

The SDK dynamically loads Axern proto definitions from `sdk/proto`. Set
`AXERN_PROTO_ROOT` when running from a different layout.

## Basic Usage

```ts
import { AxernClient, Sandbox } from "@cofy-x/axern-sdk";

const client = AxernClient.fromContext(
  process.env.AXERN_CONFIG ?? `${process.env.HOME}/.config/axern/config.json`,
  process.env.AXERN_CONTEXT,
);

const sandbox = await new Sandbox({
  client,
  image: "python:3.12-slim",
  namespace: "typescript-sdk-example",
  tunnel: {
    upstream: "127.0.0.1:8080",
    proxyPort: 8786,
  },
}).start();

try {
  const result = await sandbox.exec("python - <<'PY'\nprint('hello from axern')\nPY", {
    check: true,
  });
  console.log(result.stdoutText());

  await sandbox.writeText("/tmp/message.txt", "hello\n", { createParents: true });
  console.log(await sandbox.readText("/tmp/message.txt"));

  await sandbox.uploadDir("./fixtures", "/tmp/fixtures");
  await sandbox.downloadDir("/tmp/fixtures", "./downloaded-fixtures");
} finally {
  await sandbox.close();
  client.close();
}
```

Run a tool from a separate image with `execImage` or `processImage`. OCI and
Nydus refs use the same image field. When `mounts` is omitted, the SDK requests
`/workspace -> /workspace`; pass `mounts: []` for no shared paths. Use
`new Sandbox({ image })` when the image should be the sandbox rootfs with normal
files, exec, process, tunnel, and lifecycle APIs; image-backed processes are
temporary side processes attached to an existing sandbox.

```ts
import { workspaceMount } from "@cofy-x/axern-sdk";

const result = await sandbox.execImage("ghcr.io/cofy-x/agent:latest", "tool run", {
  check: true,
  mounts: [workspaceMount("/workspace")],
});
console.log(result.stdoutText());
```

`AxernClient` requires an explicit endpoint. Use `AxernClient.fromContext()` in
interactive examples or `AxernClient.fromEnv()` in environment-driven
automation. Neither the client constructor nor `Sandbox` silently reads the
user directory.

## Configuration

`Sandbox` requires exactly one source: `templateId`, `image`, or
`environmentId`.

Common options:

- `namespace`: control-plane namespace, default `default`
- `client`: explicit `AxernClient` shared by the sandbox
- `argv`, `env`, `cwd`: initial sandbox process configuration
- `runtimeClass`: runtime selector, for example `runsc` or `runc`
- `requestCpu`, `requestMemory`: scheduler resource requests such as `500m`
  and `512MiB`; numeric CPU values are cores and numeric memory values are bytes
- `limitCpu`, `limitMemory`: runtime cgroup resource limits such as `1` and
  `1GiB`; numeric CPU values are cores and numeric memory values are bytes
- `readyTimeoutMs`: service replica readiness timeout

Tunnel options:

- `tunnel.upstream`: local TCP address reached by the SDK connector, for
  example `127.0.0.1:8080`
- `tunnel.proxyPort`: sandbox-local port bound by Axern, default `8786`
- `tunnel.ttlSeconds`: control-plane tunnel session TTL, renewed by the SDK

Tunnel traffic reuses the client gateway endpoint, mTLS identity, server name,
and proxy policy.

After `start()`, use `sandbox.metadata.tunnel?.boundAddr` from inside the
sandbox, for example `http://127.0.0.1:8786`.

## Sandbox API

- Lifecycle: `start()`, `close()`, `state`, `metadata`
- Execution: `exec(command, options)`, `process(command, options)`
- Files: `readFile`, `readText`, `writeFile`, `writeText`, `stat`, `listDir`,
  `exists`, `mkdir`, `remove`, `copy`, `move`, `chmod`, `touch`
- Directories: `uploadDir(localPath, remotePath)`,
  `downloadDir(remotePath, localPath)`
- Tunnel: `new Sandbox({ tunnel: { upstream, proxyPort } })`,
  `metadata.tunnel`
- Errors: `AxernRpcError`, `SandboxExecError`, `SandboxStateError`,
  `SandboxValidationError`, `isNotFound`, `isPermissionDenied`, `isTimeout`,
  `rpcCode`

## Local Smoke

With the local compose stack running, verify the real SDK path with:

```bash
pnpm --filter @cofy-x/axern-sdk run smoke:local
pnpm --filter @cofy-x/axern-sdk run smoke:tunnel
```

The smoke loads `deploy/local/state/compose/axern.env` when present. It uses the
`python311` template by default; set `AXERN_TS_SMOKE_IMAGE` to verify an image
source instead.

Check package contents with:

```bash
pnpm --filter @cofy-x/axern-sdk run pack:dry-run
```

## Proto Boundary

The SDK loads protobuf definitions through `@grpc/proto-loader`. Dynamic proto
access remains isolated under `src/generated`; public callers depend only on
the stable TypeScript DTOs and error types.

## Scope

This SDK is Node.js-first. Browser support, generated TypeScript proto stubs, and
full control-plane administration APIs are intentionally left for later versions.
