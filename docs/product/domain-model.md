# Stable Domain Model

This document defines Axern's stable product identities, lifecycle ownership, and platform boundary. Internal tables, packages, transports, and caches may change, but they must preserve this model and cannot create a parallel execution lifecycle.

## Platform Boundary

Axern provides secure, rebuildable, high-concurrency environment execution for agent evaluation, training, and data synthesis. Caller workflows remain outside the Run and Allocation state machine.

The only durable execution chain is:

```text
Environment -> Run -> Allocation -> runsc sandbox
```

`Sandbox` is the SDK experience over this chain, not another control-plane resource. A new first-class object requires an independent stable identity, lifecycle or authorization boundary, durable ownership after client exit, and a demonstrated execution-platform need that existing objects cannot express.

## Public Durable Roots

| Object | Stable meaning | Authority |
| --- | --- | --- |
| `Namespace` | Authorization, quota, query, and cleanup boundary | `controld` and PostgreSQL |
| `Environment` | Immutable, rebuildable execution input | `controld`; image bytes remain in the image stack |
| `Run` | One user-visible execution request and final result | `controld` and PostgreSQL |
| `Secret` | Encrypted namespace-scoped input referenced by execution | `controld` and PostgreSQL |
| `Principal` | Stable human, workload, or administrative identity | `controld` and PostgreSQL |
| `Credential` | Expiring and revocable authentication material | `controld` and PostgreSQL |
| `RoleBinding` | Platform- or namespace-scoped authorization | `controld` and PostgreSQL |
| `Node` | Never-reused administrative identity for execution supply | `controld`; observations originate at `axnoded` |

### Namespace

A Namespace scopes Environments, Runs, Secrets, quota policy, and retained metadata. Deletion rejects live dependent state and leaves an irreversible identity tombstone so a deleted name cannot acquire a new authorization identity. Namespace is not a Kubernetes namespace, cloud account, cluster, region, or billing object.

### Environment

An Environment describes immutable execution input resolved from a template or digest-pinned image plus normalized policy. Run admission freezes the normalized source and resolved input; execution and recovery never depend on the continued existence of the reusable Environment row. A successful Run may request final rootfs sealing; the content-addressed result is published as another ordinary Environment rather than a Snapshot resource. Writable files, replicas, rollout state, services, and persistent volumes are not Environment properties.

### Run

A Run owns one immutable request, its Environment snapshot, exactly one Allocation, cancellation, public status, terminal result, failure classification, usage, and declared-output specification.

```text
PLACED -> STARTING -> RUNNING
   |          |          |
   +----------+----------+-> CANCELLED | SUCCEEDED | FAILED
```

Terminal state is irreversible. Axern does not replace a failed Allocation underneath a Run; another execution is a new Run and Allocation.

Rootfs sealing is an optional Run finalization result independent from workload status. It begins only after a successful primary workload, preserves the Run's terminal result, and resolves to `ready` with a new Environment or `failed` with a stable diagnostic. It never turns a failed or cancelled Run into a reusable image.

### Secret

Run and Environment state contain typed Secret references, never plaintext. Read APIs return metadata only, required references prevent unsafe deletion, and plaintext cannot enter logs, events, metrics, diagnostics, outputs, or runtime identity.

### Principal, Credential, And RoleBinding

Gatewayd authenticates a typed Credential, controld resolves its Principal, and RoleBindings authorize platform or Namespace operations. X.509 and SSH credentials share this model. Internal workload identities are separate from user Principals and cannot inherit user or operator authority.

### Node

Node identity is durable and independent from hostname or heartbeat freshness. Revocation withdraws authority without claiming resources are cleaned; retirement requires lifecycle blockers to converge. A replacement host receives a new Node ID, and transport credentials never create a second Node identity.

## Subordinate Execution State

These facts exist only under their owner and cannot become independent public workloads. Some are durable records, while ExecutionLease is finite authority materialized only as the node's receipt-clock deadline.

| Fact | Owner | Purpose |
| --- | --- | --- |
| `Allocation` | Run | Concrete Node binding and infrastructure convergence identity |
| Resource charge | Allocation | Committed CPU, memory, storage, and runtime-slot accounting |
| `ExecutionLease` | Allocation | Finite permission for the bound Node to keep executing |
| `AllocationAccessGrant` | Allocation | Finite purpose-scoped data-plane authority |
| `TunnelSession` | Allocation | Revocable reverse-TCP session and relay convergence |
| `CapabilityRequirement` | Allocation | Immutable required capability and loss policy |
| `CapabilityCondition` | Allocation | Rebuildable current enforcement diagnosis |
| `QuotaPolicy` | Namespace | Optional admission ceilings |
| `AuditEvent` | Administrative mutation | Actor, reason, target, and result |

### Allocation

Allocation is the unique, never-reused execution identity that binds one Run to one Node and runsc sandbox.

```text
BOUND -> STARTING -> ACTIVE -> RELEASING -> RELEASED
```

Allocation owns binding, infrastructure convergence, and resource charge, but not the Run request or public result. Resource charge remains until confirmed cleanup reaches `RELEASED`; a terminal Run or failed delete RPC is not evidence that resources are free. Runtime, cgroup, egress, SSH, Tunnel, and cleanup all use the same Allocation ID.

### ExecutionLease And AllocationAccessGrant

ExecutionLease bounds runtime liveness during partition and is enforced by axnoded. AllocationAccessGrant authorizes one purpose-scoped gateway operation. Neither is public SDK identity, neither authorizes another Allocation, and neither substitutes for the other.

### TunnelSession

TunnelSession is durable because pairing, renewal, revocation, expiry, restart convergence, and accounting outlive one TCP connection. It remains subordinate to one Allocation and cannot select replicas or recreate a Service or Route model.

### Capability Evidence

Node observations are typed, ordered, and time-bounded evidence. Allocation requirements freeze the capability and loss policy needed by one execution; conditions are rebuildable diagnoses. Missing required evidence fails closed, and no capability record creates a second lifecycle or permits runtime fallback.

## Allocation-Scoped Capabilities

Process, file, archive, terminal, SSH, and Tunnel are public Allocation capabilities, not durable root objects. Processes and interactive connections cannot outlive Allocation authority. SSH creates an Allocation-local process without running `sshd`; file and archive operations access only the Allocation filesystem.

`Sandbox` may resolve or create an Environment, create and observe a Run, expose its Allocation, perform Allocation-scoped operations, and request bounded cleanup. It must preserve the underlying identities and errors rather than maintaining competing client-side state.

## Results And Outputs

Run result is durable metadata: terminal status, exit-code knowledge, diagnostic, message, and usage. Stdout, stderr, and writable files remain node-local unless the immutable Run specification declares bounded outputs.

Declared output sealing is a cleanup barrier, not an Artifact service. The node publishes one immutable, read-only manifest and retains available bytes for a bounded period; node-disk loss remains explicit, and the caller owns durable publication. Limits and recovery semantics are defined by the [storage lifetime contract](../architecture/storage-architecture.md).

Rootfs sealing is a separate cleanup barrier for explicitly requested successful Runs. It exports the stopped Allocation's writable rootfs layer and combines it with the digest-pinned base into a content-addressed OCI image. Bind mounts, image mounts, Secret projections, kernel filesystems, processes, sockets, terminals, SSH, and Tunnel state are excluded. Controld atomically publishes the resulting ordinary Environment and Run snapshot result before acknowledging the node receipt. Axern stores no second snapshot identity or mutable workspace; registry retention and physical blob garbage collection remain operator policy.

## Fact Ownership

| Fact | Authority | Not authority |
| --- | --- | --- |
| Public resources, binding, charge, grants, requirements, and lifecycle | `controld` / PostgreSQL | Gateway cache or node recovery files |
| ExecutionLease receipt deadline | axnoded Allocation recovery record | Access-grant cache or OCI metadata |
| Runtime, mount, network, cleanup, and sealed output | axnoded node-local state | Cross-node product truth |
| Image cache and read-only mounts | imagemgr / imagefsd | Run status or writable workspace truth |
| Streams, PTY, SSH, and live connections | Node and gateway transient state | Durable result or authorization |
| Long-term output bytes | Caller-owned storage | PostgreSQL or bounded node retention |

Run and Allocation never double-own a fact: Run owns immutable execution intent and public result; Allocation owns concrete binding and infrastructure convergence; axnoded owns the observed local runtime and cleanup projection.

## Outside The Core

| Concept | Owner |
| --- | --- |
| Service, Replica, Route, Rollout | External PaaS or application |
| Function, Invoke, worker dispatch | Caller queue or workflow system |
| Persistent Workspace or Volume provisioning | External storage or workspace layer |
| Dashboard, IDE, cluster, region, cloud account, image build | Composed product or deployment infrastructure |

## Invariants

1. Environment input is immutable, verifiable, and rebuildable on an eligible Node.
2. Run alone owns user execution intent and terminal result; one Run owns one Allocation.
3. Allocation IDs are globally unique and never reused; authority and cleanup cannot cross identities.
4. Resource charge ends only after confirmed required cleanup.
5. ExecutionLease, access grants, SSH, and Tunnel authority are finite and Allocation-scoped.
6. Missing identity, isolation, capability, network, mount, or resource enforcement fails closed.
7. Node-local data is not described as durable without explicit delivery.
8. Restart or partition cannot create two authoritative owners or revive terminal state.
9. Cleanup is idempotent, owner-aware, and observable; unknown state is not safely released.
10. Caller policy remains outside the Run/Allocation state machine.

Public protobufs, SDK types, CLI nouns, and durable records must use these meanings. Internal schema or transport changes may be coordinated atomically, but compatibility exists only where the published public contract defines it. A proposed new product object must prove independent identity, lifecycle, authorization, cleanup, and a real workload that existing objects cannot express.
