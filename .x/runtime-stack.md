# Runtime Stack

Use this document only when a change crosses runtime, control-plane, gateway,
storage, SDK, or networking boundaries. For changes contained inside one
subsystem, read that subsystem's `AGENTS.md` and `README.md` instead.

## Stack Map

```text
clients / SDKs / apps
  -> gatewayd          external mTLS control edge plus terminal, SSH, tunnel, and artifact data plane
     -> controld       Environment / Run / Allocation semantics and durable control state
        -> axnoded         allocation lifecycle and sandbox execution
           -> egressd      trusted egress policy lifecycle and host enforcement
           -> imagemgr     image rootfs resolution and mount references
              -> imagefsd  read-only image data plane
        -> tunneld         durable tunnel target selection for the raw TCP relay
     -> axnoded        allocation-bound terminal, SSH, process, file, and artifact forwarding
        -> runsc        production OCI sandbox lifecycle
           -> sandboxd  sandbox PID 1, process/file/PTY/proxy APIs
        -> bpfnet       optional host networking dataplane
     -> tunneld        public-to-node raw TCP tunnel relay
```

Shared API contracts live under `sdk/proto`; generated SDK code lives under
the language SDK workspaces.

## Ownership Rules

- Control plane:
  - `control/controld` owns Environment, Run, and Allocation semantics,
    placement, node lifecycle dispatch, allocation target resolution, tunnel
    session control, and durable control-plane state. External product API
    traffic should enter through `gateway/gatewayd`.
- Gateway and tunnels:
  - `gateway/gatewayd` owns the external control API plus allocation-bound
    terminal, SSH, tunnel client, artifact, and sandbox data-plane entry.
  - Public control and sandbox requests are authenticated at gatewayd and
    authorized by controld against durable Principal credentials and scoped
    role bindings. Direct public controld access is not supported.
  - `control/controld` owns tunnel sessions and advertises a client target on
    the gateway edge plus a node target on the internal `tunneld` relay.
  - `runtime/tunneld` owns internal reverse TCP tunnel pairing. Tunnels are
    allocation-bound platform networking, not Service routing or Axrun LLM
    telemetry.
- Node runtime:
  - `runtime/axnoded` owns node-local sandbox lifecycle, OCI bundle generation,
    runtime handler integration, node operator APIs, gateway-forwarded sandbox
    operations, allocation-local writable rootfs/workspaces, and allocation
    cleanup. Its embedded recovery state tracks ownership and reservations,
    not reusable persistent volumes.
  - `runtime/egressd` owns the trusted host-side sandbox egress policy record,
    allocation-attempt fencing, persistence, recovery, reconciliation, and
    enforcement health. Its private Unix socket and bypass privileges are not
    exposed to workload namespaces.
  - `runtime/imagemgr` owns image rootfs resolution, OCI/Nydus image mount
    orchestration, imported image cache state, and mounted rootfs references.
  - `runtime/imagefsd` owns the read-only image data plane used by imagemgr.
- Network:
  - `network/bpfnet` owns optional eBPF host networking behavior.

`axern-sandboxd` runs as sandbox PID 1 for sandboxd-backed OCI bundles.
High-level sandbox operations should flow through sandboxd where available;
direct OCI runtime exec is a debug-level tool.

## Runtime Contracts

- The only durable execution chain is `Environment -> Run -> Allocation`.
  `Sandbox` in an SDK creates and controls that chain; it does not introduce a
  separate persistent object or borrow a Service lifecycle.
- Service, Function, Agent Profile, and generic Volume product lifecycles are
  outside the runtime contract. Do not restore their APIs, schemas,
  reconcilers, routes, metrics, or compatibility layers.
- Runsc is the supported execution runtime. Packaged nodes and the
  source-development stack enable only runsc; unsupported runtime classes are
  rejected without fallback.
- Workload execution config carries `runtime_class`; empty values default in
  the control/runtime path before node lifecycle dispatch.
- Workload network policy is immutable execution config. Strict policy is a
  fail-closed boundary; DNS deny is explicitly DNS-only. Controld normalizes
  the public policy and derives exact node capability requirements before
  lifecycle dispatch. The node must reject a policy workload when the matching
  egress enforcement proof is unavailable; it may never silently ignore a
  newer policy shape.
- Axnoded prepares egress policy after assigning the sandbox interface and
  before starting the OCI user process. Deletion stops the workload before
  deleting policy and releasing the interface. Startup reconciliation sends
  exact allocation ID, attempt, sandbox IP, policy digest, and execution
  revision proofs; egressd removes every orphan or mismatched record.
- `requests` drive placement, admission, and node reservation. `limits` remain
  runtime enforcement ceilings.
- `axnoded` owns the aggregate `runtime_slots` report consumed by placement and
  admission. Enabled resource pools constrain that aggregate; disabled pools
  remain node-local implementation choices and are never inferred by
  `controld`.
- Node reports without `runtime_slots` are rejected. Control-plane and node
  releases that introduce a new required node-summary contract must be rebuilt
  together; mixed-version operation is not a supported compatibility path.
- Capability protobuf numbers belong to the coordinated control-plane, node,
  and SDK release contract. Renumbered contracts require matching binaries and
  regenerated evidence; old numeric snapshots or persisted proofs must not be
  relabeled or reused as current evidence.
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
- Writable rootfs/workspace data belongs to one allocation and does not promise
  persistence across allocation replacement or node loss. Images and runtime
  recovery records retain their existing owners; durable outputs require
  explicit artifact delivery. See the [Storage Architecture](../docs/architecture/storage-architecture.md)
  for the data-lifetime and historical-data upgrade boundary.
- Agent bundle image mounts remain single bind mounts. Claude Code is bound at
  its private ABI target `/__claude_code`; axnoded's allocation-private rootfs
  projection supplies the public `/opt/axern/agents/claude-code` symlink used by
  Axrun. Both paths participate in mount conflict validation.
- High-level process, file, PTY, and managed-proxy operations flow through
  sandboxd when supported. Axern tunnel sessions are a separate networking
  primitive.

## Change Routing

| Change | Read / update |
| --- | --- |
| Public API, SDK shape, or protobuf contract | `sdk/proto`, generated SDKs, owning module, CLI/app docs |
| Placement, node registration, allocation lifecycle, runtime catalog | `control/controld`, `runtime/axnoded`, SDKs if user-facing |
| Node capability observation, catalog policy, admission evidence, or enforcement loss | `sdk/proto`, `lib/go/nodecapability`, `runtime/axnoded`, `control/controld`, CLI/SDK diagnostics |
| Sandbox DNS or strict egress lifecycle and enforcement | `runtime/egressd`, `runtime/axnoded`, `network/bpfnet`, deployment and verification surfaces |
| Ephemeral filesystem, writable-storage reservation, and node cleanup | `runtime/axnoded`, `control/controld` for resource admission, storage architecture |
| Gateway control edge, allocation terminal or SSH, tunnel client entry, artifact transfer | `gateway/gatewayd`, `control/controld`, `runtime/tunneld`, `runtime/axnoded` |
| Internal TCP tunnel relay or node-local tunnel binding | `runtime/tunneld`, `control/controld`, `runtime/axnoded` |
| Sandboxd lifecycle or process/file/PTY/proxy behavior | `runtime/axnoded`, `runtime/axnoded/docs/sandbox-daemon.md`, SDK/proto if API-visible |
| Image-backed rootfs resolution | `runtime/axnoded`, `runtime/imagemgr`, `runtime/imagefsd` |
| Image mounts or read-only image bundle injection | `sdk/proto`, affected SDKs/apps, `control/controld`, `runtime/axnoded`, `runtime/imagemgr` |
| OCI extraction, overlay mounts, registry auth, Nydus bootstrap | `runtime/imagemgr`, `runtime/imagefsd` when daemon behavior changes |
| eBPF NAT or host networking dataplane | `network/bpfnet`, `runtime/axnoded` |
| Repo-local devbox, runtime sockets, or `.dev/` stack layout | root Make files, devbox scripts, root docs, affected subsystem docs |

## Sync Rules

- If a shared API shape changes, regenerate code and update all affected
  SDKs, modules, tests, and user-facing docs together.
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
