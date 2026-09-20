# Axern SDKs

Axern SDKs expose the programmable sandbox surface for application code. The current first-class SDKs are Python, Go, and TypeScript.

The SDKs share the same platform boundary: lifecycle, execution, process, file, archive, and tunnel behavior is delegated to Axern control, node, runtime, and relay APIs. SDKs should not add remote shell fallbacks for platform-owned behavior.

The common public contract is captured as versioned fixtures in `sdk/contracts/v1`. Each language consumes the same context, source, resource, lifecycle, file/process/tunnel, and error fixtures while retaining language-idiomatic APIs. Errors expose operation, RPC code, details, retryability, and allocation identity where available. Mutating RPCs are not retried implicitly.

## API Matrix

| Capability | Python | Go | TypeScript |
| --- | --- | --- | --- |
| Sandbox source | `image` or `environment_id` | `Image` or `EnvironmentID` | `image` or `environmentId` |
| Lifecycle | `Sandbox` context manager, `start()`, `close()` | `NewSandbox`, `Start(ctx)`, `Close(ctx)` | `new Sandbox(...).start()`, `close()` |
| Async model | Sync and `AsyncSandbox` | `context.Context` | Promise APIs |
| Exec | `exec()`, `exec_stream()` | `Exec(ctx, ...)` | `exec()` |
| Process | `process()` / `AsyncSandbox.process()` | `Process(ctx, ...)` | `process()` |
| Single-file APIs | `read_bytes`, `read_text`, `write_bytes`, `write_text` | `ReadFile`, `WriteFile` | `readFile`, `readText`, `writeFile`, `writeText` |
| File metadata/ops | `stat`, `list_dir`, `exists`, `mkdir`, `remove`, `copy`, `move`, `chmod`, `touch` | `Stat`, `ListDir`, `Exists`, `Mkdir`, `Remove`, `Copy`, `Move`, `Chmod`, `Touch` | `stat`, `listDir`, `exists`, `mkdir`, `remove`, `copy`, `move`, `chmod`, `touch` |
| Directory transfer | `upload_dir`, `download_dir` | `UploadDir`, `DownloadDir` | `uploadDir`, `downloadDir` |
| Declared output | `DeclaredOutput`, manifest/download by Run ID | `DeclaredOutput`, manifest/download by Run ID | `declaredOutputs`, manifest/download by Run ID |
| Tunnel | `upstream`, `remote_port` | `OpenTunnel(ctx, TunnelOptions)` | `tunnel: { upstream, proxyPort }` |
| Capability discovery | `capability_status()` | `CapabilityStatus(ctx)` | `capabilityStatus()` |
| Computer Use | status, screenshot, display, mouse, keyboard | status, screenshot, display, mouse, keyboard | status, screenshot, display, mouse, keyboard |
| Metadata | `state`, `metadata`, `bound_addr` | `State()` | `state`, `metadata`, `metadata.tunnel` |
| Errors | `SandboxRpcError`, `SandboxExecError`, typed subclasses | `ExecError`, `IsNotFound`, `IsTimeout`, `IsValidation` | `AxernRpcError`, `SandboxExecError`, `SandboxValidationError`, helpers |
| Examples | `sdk/python/examples` | `sdk/go/examples` | `sdk/typescript/examples` |
| Verify | `make sdk-python-verify` | `make sdk-go-verify` | `make sdk-typescript-verify` |

Intentional language differences:

- Python provides both synchronous and asyncio surfaces.
- Go APIs take `context.Context` and return errors.
- TypeScript APIs are Node.js-first Promise APIs.
- Naming follows each language's conventions while preserving the same domain model.
- Go exposes lower-level Run, process-stream, and task-asset helpers used by infrastructure integrations.
- Browser automation remains caller-owned workload code; Axern exposes process and Computer Use primitives instead of a managed browser lifecycle.

## Release Gate

Run the repository-level SDK gates before preparing an SDK release:

```bash
make sdk-contract-verify
make sdk-artifact-verify
```

This target runs:

- shared `sdk/contracts/v1` fixtures in all three languages
- `make sdk-python-verify`
- `make sdk-go-verify`
- `make sdk-typescript-verify`
- `make agent-doc-check`
- clean installation of the Python wheel and sdist, npm tarball, and standalone Go module from their publishable boundaries
- package-content checks that reject repository-private Proto packages and imports

## Compose Validation

The release gate is intentionally local and package-focused. Before releasing changes that touch sandbox lifecycle, node/runtime file/process APIs, tunnel behavior, or generated protos, also run the relevant compose checks:

```bash
make local-compose-python-sdk-e2e
make sdk-go-examples-smoke
pnpm --filter @cofy-x/axern-sdk run smoke:local
pnpm --filter @cofy-x/axern-sdk run smoke:tunnel
```

If the change modifies `axnoded`, tunnel relay behavior, proto contracts, or compose images, refresh the local stack first:

```bash
make local-compose-refresh-verify
```
