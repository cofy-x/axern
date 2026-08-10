# AGENTS.md

## Purpose

This is the agent contract for `runtime/axnoded`.

Read the [Node Runtime README](README.md) for subsystem context before editing.
Keep this file rule-oriented: architecture boundaries, sync points, and
validation choices. Put operator workflows and topic explanations in the
README or `docs/`, not here.

## Architecture Rules

- Keep [`cmd/axnoded`](cmd/axnoded) as a thin daemon entrypoint.
- Keep [`internal/app`](internal/app) as the composition root: config,
  dependency construction, server registration, and lifecycle startup.
- Keep [`internal/api`](internal/api) as the gRPC/HTTP adapter layer: request
  validation, defaulting, proto mapping, dashboard handlers, and stream
  adapters.
- Keep [`internal/service`](internal/service) as the API-facing orchestration
  facade for sandbox lifecycle and node sandbox RPC glue. It must not import
  `internal/app` or `internal/api`.
- Keep service subdomains narrow and explicit:

  | Package | Owns |
  | --- | --- |
  | [`internal/service/allocation`](internal/service/allocation) | allocation start/delete lifecycle, capability dependency persistence and verification, runtime template mapping, prepared-container enforcement gate, rootfs preparation, start metrics |
  | [`internal/service/controlplane`](internal/service/controlplane) | allocation, capability-condition, and exit report shaping plus node reporter construction |
  | [`internal/service/imageprocess`](internal/service/imageprocess) | image-backed process orchestration, actor lifecycle, mount resolution, stream handling, cleanup policy |
  | [`internal/service/networking`](internal/service/networking) | sandbox network lookup, DNAT lifecycle, activation cleanup, HTTP proxy transport |
  | [`internal/service/process`](internal/service/process) | sandbox command execution, exec/process facade orchestration, metrics envelopes, process session transport, stream pump behavior |
  | [`internal/service/sandboxtarget`](internal/service/sandboxtarget) | container-to-runtime target resolution, running-state validation, shared exec-direct capability checks |
  | [`internal/service/sandboxcontrol`](internal/service/sandboxcontrol) | sandbox inspection, wait, kill, checkpoint, and cgroup stats control-plane operations |
  | [`internal/service/probes`](internal/service/probes) | readiness/liveness worker state, probe target status mapping, sandboxd probe adapters, liveness failure cleanup/report shaping |
  | [`internal/service/sandboxaccess`](internal/service/sandboxaccess) | sandbox-local file, browser, computer-use, diagnostics, and capability operations |
  | [`internal/service/startplan`](internal/service/startplan) | pure start request normalization and container request builders |
  | [`internal/service/volumes`](internal/service/volumes) | node-volume publish, unpublish, list, and reconcile orchestration |

  Do not add additional `internal/service/*` packages without updating
  `make check-architecture`.
- Keep runtime handler implementations and OCI bundle generation under
  [`internal/runtime`](internal/runtime). Shared OCI spec helpers live in
  [`internal/runtime/oci`](internal/runtime/oci), and host-side OCI runtime
  command/state helpers live in [`internal/runtime/ocihost`](internal/runtime/ocihost).
- Keep the root [`internal/runtime`](internal/runtime) package as the runtime
  facade: handler structs, runtime registration, runc/runsc entry methods, and
  tests that must access unexported handler state. Move reusable workflow logic
  into focused internal subpackages such as `bundleflow`, `launchflow`,
  `startupflow`, `execflow`, or `ocicli` instead of adding catch-all root files.
- Keep sandboxd host-client integration under
  [`internal/runtime/sandboxd`](internal/runtime/sandboxd). Runtime root files
  may call into it, but daemon API structs, capability snapshots, and provider
  dispatch helpers should stay in that package.
- Keep runtime-local writable rootfs view lifecycle under
  [`internal/runtime/rootfsview`](internal/runtime/rootfsview). It adapts
  already-resolved rootfs paths for local OCI runtimes; it must not own image
  pull, image mount, or `imagemgr` socket coordination.
- Keep observed node capability snapshot ownership under
  [`internal/nodecapability`](internal/nodecapability). Providers publish typed
  facts through that manager; they must not append strings to node summaries or
  treat a successful user allocation as probe evidence. Shared platform keys,
  provider ownership, dependencies, validity, and loss policy belong in
  [`lib/go/nodecapability`](../../lib/go/nodecapability).
- Runtime handler static features and resource requirements must come from
  `handler.Capabilities()` and `handler.Requirements()`. They are local handler
  declarations, not observed node platform capabilities and not sandboxd
  operation discovery.
- New runtimes must register through `RegisterRuntimeFactory` in
  [`internal/runtime`](internal/runtime).
- Keep rootfs and image-manager coordination in
  [`internal/langruntime`](internal/langruntime).
- Keep cgroup, network-interface, and warm-pool coordination in
  [`internal/resources`](internal/resources) and [`internal/network`](internal/network).
- Keep axnoded-owned metrics and tracing under
  [`internal/observability`](internal/observability).
- Keep the process-owned BoltDB lifecycle and low-level record primitives under
  [`internal/nodestate`](internal/nodestate). Consumers define narrow state
  capabilities at their package boundary; allocation state is stored as one
  record per allocation and must not regress to whole-map snapshots.
- Probe and allocation lifecycle workers must not wait for control-plane status
  RPCs. Keep allocation observations in the bounded, coalescing reporter queue;
  preserve terminal states across retry and expose queue pressure through
  axnoded observability.
- Keep test doubles in explicit test-support packages such as
  [`internal/runtime/runtimetest`](internal/runtime/runtimetest) or
  [`internal/storetest`](internal/storetest); production code must not import
  them.
- Do not add cross-package type aliases, var bridge re-exports, catch-all helper
  files, or new `pkg` packages unless they are genuinely support-level and
  independent from `internal`.
- `internal/service` subpackages must not import `internal/app` or
  `internal/api`; they should receive service-owned behavior through explicit
  dependency structs or callbacks.
- Run `make check-architecture` after directory, package-boundary, or layering
  changes.

## Sync Points

- Proto/API changes: update `.proto` sources, regenerate with `make protos` or
  `make protos-docker`, and update API, service, CLI, and tests together.
- Config changes: update [`config/config_test.go`](config/config_test.go),
  [Sample Configuration](docs/sample_conf.toml),
  and [Configuration](docs/configuration.md) when the operator-facing shape
  changes. Update the [Node Runtime README](README.md) only when invocation,
  endpoint summaries, or document routing changes.
- Runtime registration, capability, or requirement changes: update runtime
  status/dashboard behavior plus tests in [`internal/runtime`](internal/runtime)
  and [`internal/service/runtime_status_facade_test.go`](internal/service/runtime_status_facade_test.go).
- Observed node capability changes: update the capability proto, shared catalog,
  provider manager, inventory/reporting, create-time gate, and
  [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md)
  together. Coordinate placement and persistence changes with controld.
- Image-backed rootfs or `imagemgr` integration changes: keep socket/request
  expectations aligned with `runtime/imagemgr`; update
  [Runtime Stack](../../.x/runtime-stack.md) if the cross-runtime
  flow changes.
- Network, DNAT, eBPF, or `bpfnet` integration changes: read
  [bpfnet Agent Contract](../../network/bpfnet/AGENTS.md) before
  editing shared behavior.

## Validation

- Generic Go changes: run `make fmt`, `make vet`, and targeted `go test`.
- Directory or layering changes: also run `make check-architecture`.
- Non-Linux hosts: run `make test-host` plus Linux-target compile checks for
  affected runtime packages when full runtime tests are unavailable.
- Runtime, container lifecycle, cgroup, network, or DNAT behavior changes:
  validate in Linux with privileged container access. Prefer `make verify-docker`;
  use `make verify-docker-runsc-debug`, `make verify-docker-runc-debug`, or
  `make verify-docker-runsc-ebpf` when the change needs narrower Linux runtime
  validation.
- Demo/dashboard changes: run `make run-dashboard-nginx-demo`.

Use [Verification](docs/verification.md) for the full validation matrix
and [Runtime Scripts](scripts/README.md) for script-level knobs.
