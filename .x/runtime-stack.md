# Runtime Stack

Use this document only when a change crosses runtime, control-plane, gateway, storage, SDK, or networking boundaries. For work contained inside one subsystem, read that subsystem's `AGENTS.md` and README instead. Product object meaning remains authoritative in the [Stable Domain Model](../docs/product/domain-model.md).

## Stack Map

```text
clients / SDKs / Axrun
  -> gatewayd          unified external control and Allocation data edge
     -> controld       durable Environment / Run / Allocation authority
        -> PostgreSQL  central control state
        -> axnoded     Allocation lifecycle dispatch
     -> axnoded        process, file, archive, terminal, and SSH forwarding
     -> tunneld        Tunnel client peer

axnoded
  -> controld          status, inventory, and capability reporting
  -> tunneld           Tunnel node peer
  -> runsc             production sandbox lifecycle
     -> sandboxd       sandbox PID 1 and process/file/PTY/proxy APIs
  -> egressd / bpfnet  egress policy and host networking
  -> imagemgr
     -> imagefsd       immutable rootfs and read-only image data plane
```

Shared API contracts live under `sdk/proto`; generated client code lives in the language SDK workspaces.

## Ownership

- `control/controld` owns durable product semantics, placement, lifecycle intent, target resolution, leases, TunnelSessions, authorization decisions, and PostgreSQL state.
- `gateway/gatewayd` owns the unified external edge. It authenticates public protocols and forwards authorized control or Allocation-scoped traffic without owning placement, lifecycle, or durable product state.
- `runtime/axnoded` owns node-local Allocation execution, recovery, cleanup, writable filesystems, and gateway-forwarded sandbox operations.
- `runtime/egressd`, `network/bpfnet`, `runtime/imagemgr`, and `runtime/imagefsd` own their narrow node-local network or image contracts; none owns Run state.
- `runtime/tunneld` owns reverse-TCP peer pairing. `controld` owns the TunnelSession, while `gatewayd` and `axnoded` provide the client and node peer paths.
- Axrun and other evaluation, training, or data-synthesis systems remain callers above the execution platform.

## Cross-Component Invariants

- The only durable execution chain is `Environment -> Run -> Allocation`; SDK `Sandbox` is a facade over it.
- Runsc is the only supported production runtime. Missing isolation, policy, or required capability evidence fails closed.
- Public clients address `gatewayd`, never node targets or internal execution leases. Internal lifecycle and status traffic does not route through the gateway.
- Node capability observations, admission policy, and enforcement follow the [Observed Capability Providers](../docs/architecture/observed-capability-providers.md) contract.
- Resource requests drive placement and reservation; limits are runtime enforcement ceilings. See the [Resource Model](../docs/architecture/resource-model.md).
- Writable rootfs and workspace data is Allocation-local. Callers must download or export required outputs before cleanup; Axern has no reusable persistent Volume or generic public Artifact root.
- Images resolve through `axnoded -> imagemgr -> imagefsd` where required. Sandbox network policy resolves through `axnoded -> egressd / bpfnet`; strict policy never degrades silently.

## Change Routing

| Change | Read / update |
| --- | --- |
| Public API, SDK shape, or protobuf contract | `sdk/proto`, generated SDKs, owning module, CLI and public docs |
| Environment, Run, Allocation, placement, lease, or node lifecycle | `control/controld`, `runtime/axnoded`, affected SDK surfaces |
| Capability observation, admission evidence, or enforcement loss | `lib/go/nodecapability`, `runtime/axnoded`, `control/controld`, SDK/proto diagnostics |
| Sandbox DNS, egress policy, NAT, or host networking | `runtime/egressd`, `runtime/axnoded`, `network/bpfnet`, deployment and verification surfaces |
| Writable filesystem reservation, recovery, or cleanup | `runtime/axnoded`, `control/controld`, storage architecture |
| Public gateway, terminal, SSH, file/archive, or Tunnel client path | `gateway/gatewayd`, `control/controld`, `runtime/tunneld`, `runtime/axnoded` |
| Sandboxd process, file, PTY, or proxy behavior | `runtime/axnoded`, sandboxd documentation, SDK/proto when public |
| OCI, Nydus, rootfs, or image mounts | `runtime/axnoded`, `runtime/imagemgr`, `runtime/imagefsd`, SDK/proto when public |
| Devbox sockets or `.dev/` stack layout | Root Make files, devbox scripts, root docs, affected service docs |

## Synchronization And Validation

- A shared contract change updates generated code, callers, tests, and authoritative documentation together. Protobuf generation must finish before consumers compile.
- A socket, mount target, or `.dev/` layout change updates every owning module that names it.
- Run the owning subsystem checks first, then the Linux, Compose, kind, or regional truth path required by the changed behavior. For `.x` changes, run `make agent-doc-check`.
