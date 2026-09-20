# Axern TypeScript SDK

Node.js SDK for programmable Axern sandboxes.

This first SDK surface is intentionally focused on the programmable sandbox path:

- create or attach a `Sandbox`
- run `exec`
- start attached `process` streams
- discover optional providers with `capabilityStatus`
- use Computer Use status, display, screenshot, mouse, and keyboard APIs
- use platform file RPCs such as `readFile`, `writeFile`, `stat`, and `listDir`
- transfer directories with archive-backed `uploadDir` and `downloadDir`
- expose local services to the sandbox with `tunnel`

## Install

Install the published package in a Node.js project:

```bash
pnpm add @cofy-x/axern-sdk
```

For repository development:

```bash
pnpm install
make sdk-typescript-verify
```

The SDK dynamically loads Axern proto definitions from `sdk/proto`. Set `AXERN_PROTO_ROOT` when running from a different layout.

## Basic Usage

```ts
import { AxernClient, Sandbox } from "@cofy-x/axern-sdk";

const client = AxernClient.fromContext(process.env.AXERN_CONFIG ?? `${process.env.HOME}/.config/axern/config.json`, process.env.AXERN_CONTEXT);

const sandbox = await new Sandbox({
  client,
  image: "docker.io/library/python:3.12-slim",
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

## Network Policies

Omitting `networkPolicy` requests unrestricted networking. Strict policies are fail-closed; `denyDns` only refuses matching traditional UDP/TCP DNS queries and does not block direct IP traffic, DoH, DoT, or already-resolved addresses.

```ts
import { NetworkPolicy, Sandbox } from "@cofy-x/axern-sdk";

const sandbox = await new Sandbox({
  client,
  image: "docker.io/library/python:3.12-slim",
  networkPolicy: NetworkPolicy.denyDns("github.com", "*.github.com", "githubusercontent.com", "*.githubusercontent.com"),
}).start();
```

`NetworkPolicy.allowDomains("example.com", "*.example.com")` allows only strict HTTP/HTTPS destinations validated by DNS plus HTTP Host or TLS SNI. `NetworkPolicy.strict({ cidrRules: [...] })` adds explicit TCP/UDP CIDR and port grants; `NetworkPolicy.denyAll()` allows no egress.

`AxernClient` requires an explicit endpoint. Use `AxernClient.fromContext()` in interactive examples or `AxernClient.fromEnv()` in environment-driven automation. Neither the client constructor nor `Sandbox` silently reads the user directory.

## Configuration

`Sandbox` requires exactly one source: `image` or `environmentId`.

Common options:

- `namespace`: control-plane namespace, default `default`
- `client`: explicit `AxernClient` shared by the sandbox
- `argv`, `env`, `cwd`: initial sandbox process configuration
- `requestCpu`, `requestMemory`, `requestEphemeralStorage`: scheduler resource requests such as `500m`, `512MiB`, and `1GiB`; numeric CPU values are cores and numeric memory/storage values are bytes
- `limitCpu`, `limitMemory`, `limitEphemeralStorage`: runtime hard limits; numeric CPU values are cores and numeric memory/storage values are bytes
- `readyTimeoutMs`: Run allocation startup timeout
- `declaredOutputs`: bounded files or directory-as-tar outputs sealed before runtime cleanup

Tunnel options:

- `tunnel.upstream`: local TCP address reached by the SDK connector, for example `127.0.0.1:8080`
- `tunnel.proxyPort`: sandbox-local port bound by Axern, default `8786`
- `tunnel.ttlSeconds`: control-plane tunnel session TTL, renewed by the SDK

Tunnel traffic reuses the client gateway endpoint, mTLS identity, server name, and proxy policy.

After `start()`, use `sandbox.metadata.tunnel?.boundAddr` from inside the sandbox, for example `http://127.0.0.1:8786`.

## Sandbox API

- Lifecycle: `start()`, `close()`, `state`, `metadata`
- Execution: `exec(command, options)`, `process(command, options)`
- Agent sandbox: `capabilityStatus`, `computerUseStatus`, `computerUseScreenshot`, `computerUseDisplay`, `computerUseMouse`, `computerUseKeyboard`
- Files: `readFile`, `readText`, `writeFile`, `writeText`, `stat`, `listDir`, `exists`, `mkdir`, `remove`, `copy`, `move`, `chmod`, `touch`
- Directories: `uploadDir(localPath, remotePath)`, `downloadDir(remotePath, localPath)`
- Tunnel: `new Sandbox({ tunnel: { upstream, proxyPort } })`, `metadata.tunnel`
- Errors: `AxernRpcError`, `SandboxExecError`, `SandboxStateError`, `SandboxValidationError`, `isNotFound`, `isPermissionDenied`, `isTimeout`, `rpcCode`

## Local Smoke

With the local compose stack running, verify the real SDK path with:

```bash
pnpm --filter @cofy-x/axern-sdk run smoke:local
pnpm --filter @cofy-x/axern-sdk run smoke:tunnel
```

The smoke loads `deploy/local/state/compose/axern.env` when present and requires `AXERN_TS_SMOKE_IMAGE` to name the workload OCI image.

Check package contents with:

```bash
pnpm --filter @cofy-x/axern-sdk run pack:dry-run
```

## Proto Boundary

The SDK loads protobuf definitions through `@grpc/proto-loader`. Dynamic proto access remains isolated under `src/generated`; public callers depend only on the stable TypeScript DTOs and error types.

## Scope

This SDK is Node.js-first. Browser automation runs as caller-owned workload software through process and Computer Use operations; generated TypeScript proto stubs and full control-plane administration APIs remain outside its public contract.

## Declared And Stream Output

Pass `declaredOutputs` when creating a Run or Sandbox, then use `getSealedOutputManifest(runId)` and `downloadSealedOutput(runId, outputId, writable)` after the Run becomes terminal. Full downloads verify size and SHA-256 while respecting the destination stream's backpressure. Declared output is Node-local for 15 minutes after cleanup starts: it survives axnoded restart but not Node-disk loss. Limits are 16 paths, 64 MiB per file, 256 MiB per tar, and 256 MiB total. It is not a persistent workspace or object store.

## Reusable Rootfs Environment

Set `rootfsSnapshot: true` on a finite `createRun()` call, then call `waitRootfsSnapshot(runId)` after the workload succeeds. The returned `environment_id` names a normal Environment that can start multiple fresh copy-on-write Runs. The content-addressed image contains writable rootfs changes only; bind and image mounts, Secret projections, kernel filesystems, processes, sockets, and sessions are excluded. Failed or cancelled Runs produce no Environment. The option is deliberately absent from `Sandbox`, whose close path cancels its long-lived Run.

Attached Process queues at most 64 unread events, pauses the gRPC stream at the bound, and resumes below the low-water mark. `write`, `closeStdin`, signal, and resize promises resolve only after the gRPC write callback. Closing an attached process attempts `TERM`; durable workload termination remains Run cancellation. `Sandbox.close()` reports cleanup failures and does not wait for a terminal Run, so call `waitRun()` when terminal confirmation is required.
