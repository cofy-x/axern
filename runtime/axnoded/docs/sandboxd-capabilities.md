# Sandboxd Capabilities And Providers

`axern-sandboxd` is the default sandbox-local control plane for OCI workloads
created by `axnoded`. This matrix records which capabilities are baseline
platform behavior and which capabilities depend on the runtime image/profile.

SDKs and CLIs must continue to call Axern product APIs. They may use the stable
provider names returned by product APIs, but they must not depend on sandboxd
socket paths, daemon HTTP endpoints, daemon-local labels, or internal provider
implementation details.

The public discovery surface is `NodeSandbox.CapabilityStatus` and SDK helpers
such as `sandbox.CapabilityStatus(ctx)` / `sandbox.capability_status()`. It is a
safe summary of readiness, capabilities, providers, dependencies, and provider
counts. Raw daemon diagnostics remain operator-only.

## Ownership

| Area | Owner | Contract |
| --- | --- | --- |
| OCI lifecycle | `runc` / `runsc` via `axnoded` | Runtime create, wait, kill, delete, isolation, namespaces, cgroups, and mounts. |
| Sandbox-local control | `axern-sandboxd` | PID 1 supervision, process execution, terminal sessions, files, probes, diagnostics, and optional desktop/browser operations. |
| Product access | `axnoded` / control plane | Authorization, allocation identity, attempts, leases, routing, and gRPC error mapping. |
| Public SDKs | SDK packages | Language-native sandbox APIs only; no raw daemon transport or endpoint exposure. |

## Baseline Capabilities

These are part of normal sandboxd-backed OCI behavior. Missing readiness or a
missing baseline capability is a platform failure and should fail closed.

| Capability | Product Surface | Daemon Surface |
| --- | --- | --- |
| PID 1 supervision | lifecycle create/wait/kill/delete | entrypoint metadata, signal forwarding, follow-exit status |
| readiness and diagnostics | create readiness, operator diagnostics | `/diagnostics`, `/readyz`, `/status`, `/capabilities` |
| capability discovery | SDK/node capability status | brokered `NodeSandbox.CapabilityStatus` |
| file and archive | SDK/node file APIs | `/files/*`, `/files/archive/*` |
| process | SDK/node process APIs | `/processes`, wait, signal, stdin, stream |
| terminal / PTY | SSH and exec-stream terminal flows | terminal process creation, stream, resize, stdin-close |
| probes | service readiness/liveness | `/probe` |
| ports and mounts diagnostics | internal diagnostics and failure reports | `/ports`, `/mounts` |

## Optional Capabilities

Optional capabilities are image/profile features. They are discoverable through
daemon diagnostics and are dispatched only when available. Generic sandbox
startup must not fail only because an optional capability is absent.

| Capability | Availability Rule | Product Surface |
| --- | --- | --- |
| `computer_use` | Desktop session and required display/input/screenshot tools are present. | Desktop status, screenshot, display, mouse, and keyboard APIs. |
| `browser` | Desktop/browser-capable image has a supported browser command or profile open hook. | Browser status, open, close, navigate, resize, click, type, and wait APIs. |
| VNC / noVNC | Future desktop transport profile explicitly enables it and Axern brokers access. | Future authorized desktop transport APIs. |

## Runtime Image Matrix

The runtime image does not decide whether sandboxd is injected. Sandboxd is the
default PID 1 control plane for OCI workloads. Images only affect optional
provider availability.

| Image / Profile | Baseline Sandboxd | `computer_use` | `browser` | Notes |
| --- | --- | --- | --- | --- |
| generic OCI image | yes | unavailable unless the image supplies a display session and helper tools | unavailable unless a supported browser command or open hook is present | Ordinary user images still get lifecycle, file, process, PTY, probes, and diagnostics. |
| `python311` | yes | unavailable | unavailable | Language runtime profile for code execution and file/process APIs. |
| `server-base` | yes | unavailable | unavailable | Verified server/service profile with SSH terminal semantics, nginx, supervisord, and sudo expectations. |
| `desktop-base` | yes | available | available when browser packages and launch hook are installed | Verified desktop profile for computer-use and browser APIs. |
| custom desktop image | yes | available when dependencies pass provider probe | available when dependencies pass provider probe | Must satisfy the same provider dependency contract as `desktop-base`. |

## Provider Rules

- Keep daemon HTTP and Unix-socket transport private to `axnoded`.
- Treat `internal/sandboxd/wire` as the source of truth for baseline, optional,
  and provider-group capability lists.
- Route product calls through typed runtime/service clients so daemon errors map
  to stable gRPC and SDK errors.
- Keep baseline capability checks strict, and keep optional provider failures
  isolated to the requested operation.
- Add or change a product-visible capability with unit tests, focused sandboxd
  E2E, and product API or SDK tests.

## Provider State Contract

Provider state is a stable daemon contract and must remain small:

| State | Dispatch | Meaning |
| --- | --- | --- |
| `available` | yes | Provider dependencies are satisfied and the capability is ready for normal operations. |
| `degraded` | yes | Provider can handle requests but reports a warning or recoverable condition through `reason` or `lastError`. |
| `unavailable` | no | Provider dependencies are missing or the image/profile cannot support the capability. |

Provider descriptors must include `name`, `state`, `available`, and the
capabilities they own. Optional `backend`, `command`, `reason`, `lastError`,
and `dependencies` fields are diagnostic details, not dispatch keys. The flat
capability list may include capabilities for `available` and `degraded`
providers only.

Dependency entries report a stable dependency name, availability, and a concise
reason when unavailable. Service, CLI, and SDK error surfaces may show these
dependency details, but they must not expose daemon socket paths or raw daemon
HTTP endpoints.

## Product Error Semantics

Sandboxd daemon errors use a structured JSON envelope:

```json
{"error":{"code":"invalid_argument","message":"invalid browser request"}}
```

`axnoded` maps daemon error codes to stable product gRPC/SDK errors through the
typed runtime sandboxd client.

| Daemon Code | Meaning |
| --- | --- |
| `invalid_argument` | Malformed request, strict JSON failure, or provider request validation failure. |
| `not_found` | Requested process, path, resource, or endpoint resource does not exist. |
| `already_exists` | File/archive resource exists and overwrite was not allowed. |
| `unavailable` | Provider or dependency is unavailable in this image/profile. |
| `failed_precondition` | Request is valid but current sandbox state cannot satisfy it. |
| `command_failed` | Sandboxd accepted the request but an in-sandbox helper command failed. |
| `timeout` | Daemon-owned wait/stream operation timed out. |
| `method_not_allowed` | Endpoint exists but method is wrong. |
| `internal_error` | Daemon bug or unexpected infrastructure failure. |

| Sandboxd Condition | Product/SDK Shape |
| --- | --- |
| Baseline capability missing | `FAILED_PRECONDITION` for the requested sandbox operation. |
| Optional provider unavailable | `FAILED_PRECONDITION` scoped to that provider operation; sandbox lifecycle remains healthy. |
| Invalid request | `INVALID_ARGUMENT` with the product operation name. |
| Missing file, process, browser session, or resource | `NOT_FOUND` when the product concept is absent. |
| Existing file/archive conflict | `ALREADY_EXISTS` when overwrite is not allowed. |
| Daemon timeout or command failure | `FAILED_PRECONDITION` with provider diagnostics attached when available. |

SDKs should keep these shapes language-native while preserving the same
operation names and error classes across sync and async clients. When provider
diagnostics are attached, SDKs should expose structured capability/provider
details in addition to the raw message so callers can detect missing browser or
computer-use dependencies without parsing strings.

## Extension Checklist

1. Define wire constants and request/response structs.
2. Implement daemon provider behavior and diagnostics.
3. Add typed `internal/runtime/sandboxd` client methods.
4. Add service/runtime dispatch through capability checks.
5. Update focused verification and product-visible tests.
6. Update SDK structured error parsing when a product-visible error detail shape
   changes.

## Verification

Use [Verification](verification.md) as the authoritative validation matrix.
Capability and provider changes usually start with focused sandboxd tests, then
add product API, SDK, or compose checks when behavior is user-visible.
