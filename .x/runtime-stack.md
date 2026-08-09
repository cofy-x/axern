# Runtime Stack

Use this document only when a change crosses runtime, control-plane, gateway,
storage, SDK, or networking boundaries. For changes contained inside one
subsystem, read that subsystem's `AGENTS.md` and `README.md` instead.

## Stack Map

```text
clients / SDKs / apps
  -> gatewayd          external mTLS identity, control, tunnel, service HTTP, and terminal edge
     -> controld       product API semantics and durable control state
        -> storaged        storage planning and binding
        -> gatewayd        Function worker dispatch through the data-plane edge
        -> tunneld         internal raw TCP tunnel relay targets
        -> axnoded         node lifecycle and sandbox execution
           -> volumed      node-local volume publish/unpublish
           -> imagemgr     image rootfs resolution and mount references
              -> imagefsd  read-only image data plane
     -> axnoded        service HTTP and terminal data-plane forwarding
        -> runc/runsc   OCI container lifecycle
           -> sandboxd  sandbox PID 1, process/file/PTY/proxy APIs
        -> bpfnet       optional host networking dataplane
```

Shared API contracts live under `sdk/proto`; generated SDK code lives under
the language SDK workspaces.

## Ownership Rules

- Control plane:
  - `control/controld` owns product API semantics, placement, allocation
    registry, node lifecycle dispatch, route resolution, tunnel session
    control, and durable control-plane state. External product API traffic
    should enter through `gateway/gatewayd`.
  - `control/storaged` owns Storage V1 semantics: volume classes, claims,
    bindings, topology, and resolved node volume specs.
- Gateway and tunnels:
  - `gateway/gatewayd` owns external control API, tunnel client entry, service
    HTTP, and browser terminal entry.
  - Public control and sandbox requests are authenticated at gatewayd and
    authorized by controld against durable Principal credentials and scoped
    role bindings. Direct public controld access is not supported.
  - `control/controld` may call `gateway/gatewayd` for gateway-owned data-plane
    actions such as Function worker dispatch. This is an internal orchestration
    path, not the external product API entry path.
  - `control/controld` owns tunnel sessions and advertises a client target on
    the gateway edge plus a node target on the internal `tunneld` relay.
  - Managed Axrun workers use distinct mTLS connections: private lease and
    rollout-worker control calls go directly to `controld`, while allocation
    and sandbox execution use the public API path through `gatewayd`.
    The worker's `rollout_executor` identity requires the active durable work
    lease as a namespace-scoped execution delegation.
  - `runtime/tunneld` owns internal reverse TCP tunnel pairing. Tunnels are
    platform networking, not Axrun LLM telemetry.
- Node runtime:
  - `runtime/axnoded` owns node-local sandbox lifecycle, OCI bundle generation,
    runtime handler integration, node operator APIs, gateway-forwarded sandbox
    operations, and allocation cleanup.
  - `runtime/volumed` owns physical node volume publish, unpublish, safe
    Claim-owned deletion, reconcile, and provider health.
  - `runtime/imagemgr` owns image rootfs resolution, OCI/Nydus/OSS image mount
    orchestration, imported image cache state, and mounted rootfs references.
  - `runtime/imagefsd` owns the read-only image data plane used by imagemgr.
- Network:
  - `network/bpfnet` owns optional eBPF host networking behavior.

`axern-sandboxd` runs as sandbox PID 1 for sandboxd-backed OCI bundles.
High-level sandbox operations should flow through sandboxd where available;
direct OCI runtime exec is a debug-level tool.

## Runtime Contracts

- Workload execution config carries `runtime_class`; empty values default in
  the control/runtime path before node lifecycle dispatch.
- `requests` drive placement, admission, and node reservation. `limits` remain
  runtime enforcement ceilings.
- `axnoded` owns the aggregate `runtime_slots` report consumed by placement and
  admission. Enabled resource pools constrain that aggregate; disabled pools
  remain node-local implementation choices and are never inferred by
  `controld`.
- Node reports without `runtime_slots` are rejected. Control-plane and node
  releases that introduce a new required node-summary contract must be rebuilt
  together; mixed-version operation is not a supported compatibility path.
- Node platform capability follows the shared
  [Observation, Policy, And Enforcement Contract](../docs/architecture/observed-capability-providers.md).
  Axnoded publishes one atomic typed observation snapshot, the shared catalog
  derives workload-facing policy, controld repeats eligibility inside the
  locked admission transaction, and axnoded verifies admitted dependencies
  before and after runtime creation. Platform requirements are derived from
  workload semantics; users may request only exact-match extension
  capabilities.
- Node identities have a durable `active` or `retired` lifecycle in Postgres.
  Retirement is an audited, irreversible control-plane operation; retired
  identities cannot register, report, authenticate, or receive new placement.
  Replacement hosts use a new node ID.
- Catalog templates and environments are runtime-neutral. Runtime-specific
  behavior belongs in execution config, runtime templates, or node runtime
  code.
- Image-backed rootfs flows resolve through `axnoded -> imagemgr -> imagefsd`
  where needed.
- High-level process, file, PTY, and managed-proxy operations flow through
  sandboxd when supported. Axern tunnel sessions are a separate networking
  primitive.

## Change Routing

| Change | Read / update |
| --- | --- |
| Public API, SDK shape, or protobuf contract | `sdk/proto`, generated SDKs, owning service, CLI/app docs |
| Placement, node registration, allocation lifecycle, runtime catalog | `control/controld`, `runtime/axnoded`, SDKs if user-facing |
| Node capability observation, catalog policy, admission evidence, or enforcement loss | `sdk/proto`, `lib/go/nodecapability`, `runtime/axnoded`, `control/controld`, CLI/SDK diagnostics |
| Storage API, volume claims/classes/bindings, node volume specs | `control/storaged`, `control/controld`, `runtime/volumed`, `runtime/axnoded` |
| Gateway control edge, tunnel client entry, service HTTP, browser terminal entry | `gateway/gatewayd`, `control/controld`, `runtime/tunneld`, `runtime/axnoded` |
| Internal TCP tunnel relay or node-local tunnel binding | `runtime/tunneld`, `control/controld`, `runtime/axnoded` |
| Sandboxd lifecycle or process/file/PTY/proxy behavior | `runtime/axnoded`, `runtime/axnoded/docs/sandbox-daemon.md`, SDK/proto if API-visible |
| Image-backed rootfs resolution | `runtime/axnoded`, `runtime/imagemgr`, `runtime/imagefsd` |
| Image mounts or read-only image bundle injection | `sdk/proto`, affected SDKs/apps, `control/controld`, `runtime/axnoded`, `runtime/imagemgr` |
| OCI extraction, overlay mounts, registry auth, Nydus bootstrap | `runtime/imagemgr`, `runtime/imagefsd` when daemon behavior changes |
| eBPF NAT or host networking dataplane | `network/bpfnet`, `runtime/axnoded` |
| Repo-local devbox, runtime sockets, or `.dev/` stack layout | root Make files, devbox scripts, root docs, affected subsystem docs |

## Sync Rules

- If a shared API shape changes, regenerate code and update all affected
  SDKs, services, tests, and user-facing docs together.
- If a shared socket path or `.dev/` layout changes, update root docs and every
  subsystem doc that names that path.
- If `axnoded` changes image-manager integration behavior, update axnoded and
  imagemgr docs together.
- If a public workload shape or sandbox-local operation changes, update the API,
  affected SDK/app docs, and the owning runtime documentation together.
- If node capability keys, evidence, provider ownership, validity, loss policy,
  or requirement derivation changes, update the canonical observed-capability
  architecture document and both controld and axnoded contracts together.

## Validation Pointers

- Prefer subsystem verification targets for local changes.
- For cross-runtime behavior, use the owning subsystem's validation matrix plus
  compose/devbox smoke targets that exercise the boundary.
- For docs touching `.x`, run `make agent-doc-check`.
