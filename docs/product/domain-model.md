# Stable Domain Model

This document is the normative model for Axern product objects and their ownership. It defines stable meaning, not a promise that every internal table, package, or RPC name is frozen.

Use this model when designing public APIs, protobuf contracts, SDKs, CLI commands, control-plane state, runtime boundaries, authorization, retention, and recovery. Current implementation details may be simplified or renamed, but they must not create a second execution lifecycle beside this model.

## Platform Boundary

Axern is an open-source environment execution platform for agent evaluation, training, and executable data synthesis. It provides secure, rebuildable, high-concurrency sandboxes through a unified SDK.

Axern owns execution infrastructure. Benchmark selection, model/provider configuration, agent policy, verification, scoring, trajectory interpretation, dataset construction, and training loops belong to Axrun, Openbench, or another caller above the platform.

The only durable execution chain is:

```text
Environment -> Run -> Allocation -> runsc sandbox
```

`Sandbox` is the primary SDK experience over that chain. It is not a separate durable control-plane resource.

## Object Classes

Axern deliberately distinguishes four classes of object:

1. public durable roots that users or administrators manage;
2. durable subordinate execution records owned by a root object;
3. allocation-scoped interaction capabilities;
4. higher-level objects that remain outside the execution platform.

A concept does not become a product object merely because it has a struct, a database row, or a possible future use. A new first-class object requires a stable identity, an independent lifecycle or authorization boundary, durable ownership after client exit, and a direct role in environment execution that cannot be expressed by an existing object.

## Public Durable Roots

| Object | Stable meaning | Owner and authority |
| --- | --- | --- |
| `Namespace` | Authorization, quota, query, and cleanup boundary for tenant-scoped execution state | `controld` and PostgreSQL |
| `Environment` | Immutable, rebuildable execution input resolved from a template or digest-pinned image | `controld`; image material is supplied by the node image stack |
| `Run` | One user-visible execution lifecycle and the sole owner of status, cancellation, exit result, failure classification, and usage | `controld` and PostgreSQL |
| `Secret` | Encrypted namespace-scoped input referenced by an Environment or Run | `controld` and PostgreSQL |
| `Principal` | Stable human, workload, or administrative identity independent from its current credential | `controld` and PostgreSQL |
| `Credential` | Rotatable or revocable authentication material for a Principal | `controld` and PostgreSQL |
| `RoleBinding` | Platform- or namespace-scoped authorization assigned to a Principal | `controld` and PostgreSQL |
| `Node` | Administrative identity for one unit of execution supply, with an audited active/revoked/retired lifecycle | `controld`; observations originate from `axnoded` |

Built-in templates are deployment-managed, read-only inputs used while resolving an Environment. They have no public identity, lifecycle, API, or database table. Caller-supplied tool or agent images use ordinary read-only image mounts and do not become product objects.

### Namespace

A Namespace scopes Environments, Runs, Secrets, quota policy, and retained metadata. Namespace deletion must reject live operational state, including an active Run, non-released Allocation, TunnelSession, or Secret. Deletion is an irreversible security tombstone: the Namespace row remains to preserve retained references and prevents the deleted name from acquiring a new authorization identity.

Namespace is not an alias for a Kubernetes namespace, cluster, region, cloud account, project-management system, or billing account.

### Environment

An Environment answers what reproducible input a Run starts from. Its source is a versioned template or an image resolved to immutable identity before execution. Read-only image mounts and normalized execution policy may be part of the resolved input; allocation-local writable files are not.

Environment meaning is immutable. A source or policy change creates a new Environment. Run creation atomically freezes both normalized source intent and the resolved input required to interpret and rebuild that Run; execution and recovery consume the Run-owned snapshot. Deleting an Environment physically removes that reusable source record without changing any admitted Run.

An Environment does not contain replicas, rollout, service discovery, readiness policy, an Agent Profile, or a persistent Volume declaration.

### Run

A Run is the smallest complete unit of execution visible to a user or SDK. It owns one immutable execution request, its admitted Environment snapshot, exactly one Allocation, cancellation, terminal result, and failure classification. Axern currently stores no durable output object or output reference on Run.

The stable state projection is:

```text
PLACED -> STARTING -> RUNNING
   |          |          |
   +----------+----------+-> CANCELLED
                           +-> SUCCEEDED
                           +-> FAILED
```

These are the public Run states. Allocation has concrete binding, execution, releasing, and released states; internal detail must project unambiguously to the owning Run. Run has no replica convergence, rolling update, readiness, degraded-service, or desired-state controller.

One Run owns one immutable Allocation identity. Retries of an idempotent lifecycle operation address that same Allocation and must match the Run's frozen request. Axern does not reschedule a Run onto a replacement Allocation: an infrastructure failure terminates the Run, and another execution or episode is a new Run with a new Allocation ID.

### Secret

Secret plaintext is accepted only by authorized creation or use paths. Normal read APIs return metadata, never plaintext. Run and Environment state store typed references, not secret content. A required reference prevents deletion while its Environment or non-terminal Run can still consume the Secret; optional Run references do not. Plaintext must not enter logs, events, metrics, diagnostics, or artifacts.

Deletion, rotation, and missing-reference behavior must be explicit. Axern does not silently substitute a same-named newer value for an already frozen Run.

### Principal, Credential, And RoleBinding

Gatewayd authenticates an external credential; controld resolves it to a durable Principal and applies RoleBindings. X.509 certificate and SSH public-key fingerprints identify typed, expiring, revocable Credentials, not permanent business identity. WebSocket Terminal uses client mTLS; SSH uses the same Principal and namespace role model, without a separate gateway-wide access list.

Platform workload identities for gatewayd, axnoded, and tunneld remain distinct from user Principals. Internal services must not reuse an external client credential when calling controld.

### Node

Node identity is durable and independent from heartbeat freshness and hostnames. Explicit deployment bindings map a Kubernetes node name to a never-reused Node ID. Emergency revocation is an audited, irreversible `active -> revoked` transition that withdraws authority even while Allocations exist; it neither releases resource charges nor certifies cleanup. Retirement is an audited, irreversible administrative transition and requires Allocations, access grants, tunnels, lifecycle delivery, and cleanup blockers to converge. A replacement host uses a new Node ID; a stale host must not regain authority by reusing a retired identity.

## Durable Subordinate Execution Records

These records need durable identity and state, but users cannot create them independently from their owner.

| Object                 | Direct owner            | Purpose                                                              |
| ---------------------- | ----------------------- | -------------------------------------------------------------------- |
| `Allocation`           | Run                     | Concrete node binding and infrastructure convergence identity        |
| Allocation resource charge | Allocation          | Committed namespace and node resource usage on the Allocation row    |
| `ExecutionLease`       | Allocation              | Finite control-plane authority for the bound node to keep executing  |
| `AllocationAccessGrant` | Allocation             | Short-lived gateway authority for one Allocation data-plane access   |
| `TunnelSession`        | Allocation              | Revocable reverse-TCP session and relay convergence state            |
| `CapabilityRequirement` | Allocation              | Immutable capability key and platform-owned loss policy              |
| `CapabilityCondition`   | Allocation              | Latest rebuildable satisfaction or enforcement diagnosis             |
| `QuotaPolicy`          | Namespace               | Optional CPU, memory, and ephemeral-storage admission ceilings       |
| `AuditEvent`           | Administrative mutation | Actor, reason, target, and result for security-sensitive writes      |
| `OperationalEvent`     | Owning domain           | Bounded lifecycle or rejection history, distinct from operator audit |

### Allocation

Allocation is the only concrete execution target. Its globally unique, never-reused ID binds its owning Run to one Node and owns only infrastructure lifecycle: `BOUND -> STARTING -> ACTIVE -> RELEASING -> RELEASED`. The node may report `STOPPED`; controld commits the Run result and moves the Allocation directly to `RELEASING` in one transaction. `STOPPED` is therefore an observation, not a durable Allocation state.

Allocation does not copy the Run request, public status, result, diagnostic, message, or optimistic version. Node creation resolves the immutable request through `Allocation -> Run`; terminal, SSH, Tunnel, process, file, and administrative lifecycle operations select the Allocation explicitly.

Allocation cannot exist as a durable orphan or an independently created public workload. A Run is not fully released until runtime, network, mounts, execution authority, access grants, tunnels, resource charges, and node recovery ownership have converged.

### Allocation resource charge

Reservation is not an independent domain object or table. Immutable CPU, sandbox-memory, and ephemeral-storage requests are stored once on the Allocation created by the admission transaction. Every non-`RELEASED` Allocation contributes that charge to Namespace and Node admission; runtime slots count the same Allocation identity.

A failed or timed-out delete RPC is not evidence that resources are free. The charge stops only after confirmed node cleanup moves the Allocation to `RELEASED`; committing a terminal Run result alone is insufficient.

### Node Transport Identity

Node admission, revocation, and irreversible retirement belong to the Node row. A one-hour enrollment token authorizes exactly one CSR, with a bounded transaction receipt for retry recovery. Node keys originate on the node and certificates renew under the exact Node URI; certificates do not create a second Node lifecycle. Normal NodeControl RPCs use verified URI identity plus current admission, never bootstrap tokens, CN, DNS aliases, or OCI metadata. Execution authority remains Allocation-scoped and finite even when certificate renewal is unavailable. Bootstrap material is a separate read-only file, never long-running configuration; after certificate publication it is not read. Renewal is a reconstructible deadline-driven task, not a durable queue. Revocation blocks new placement, access grants, Tunnel creation/renewal and certificate renewal; already issued authority is bounded by existing TTLs and peer revalidation. In-flight requests may complete within those bounds. A compromised host requires external fencing: certificate revocation is not physical proof of stopped execution.

### ExecutionLease

ExecutionLease is finite liveness authority for exactly one Allocation on its bound Node. Each successful authenticated node heartbeat returns the complete currently authorized Allocation set and a TTL. Axnoded measures the deadline from its local receipt clock, persists it with the sole Allocation recovery record, and stops an expired Allocation fail-closed. A missing grant never erases existing finite authority: responses can race new creates. Renewal only extends explicitly granted, still-valid deadlines; a late response cannot revive expired authority. There is no separate control-plane lease table, generation, or revision.

`AllocationAccessGrant` is separate short-lived data-plane authority. PostgreSQL stores its token hash, Allocation ID, Node ID, exact operation purpose, expiry, revocation and per-Node delivery revision; plaintext is returned only to gatewayd. The node watches hash-only grants and acknowledges a grant before consuming input or producing output. Public SDKs receive neither mechanism, and authority for one Allocation never authorizes another.

### TunnelSession

TunnelSession is a durable, subordinate session because relay pairing, renewal, revocation, expiry, restart convergence, and traffic accounting outlive one connection. It is always bound to one Allocation.

The public session exposes the selected client relay endpoint and observable lifecycle only. The control plane persists the selected node relay endpoint as private recovery intent and delivers it through the node control stream; the caller's local upstream remains connector-local and is never part of control-plane state.

TunnelSession does not accept Service identity, choose a replica, or recreate a `/svc` route. Allocation termination, authorization revocation, or TTL expiry invalidates the session.

### Capability Evidence

Node capability observations are typed, ordered, and time-bounded platform facts. Allocation requirements freeze only the exact key and platform loss policy for one execution. Conditions are complete projections ordered by `observed_at`; they copy neither requirements nor Node evidence and never own lifecycle.

Capability data is security and placement evidence, not a free-form user label. Missing required evidence fails closed. Axern never silently falls back from runsc to a weaker runtime.

## Allocation-Scoped Capabilities

Process, file, archive, terminal, SSH, and sandbox operations are important public capabilities, not independent durable root objects.

- A process may have a node-local ID, status, streams, and signal operations; it cannot outlive its Allocation.
- A terminal is an interactive PTY process projection.
- SSH is a gateway compatibility protocol that creates an Allocation-local shell or exec process. The sandbox does not run `sshd`.
- File and archive operations access the Allocation-private filesystem. They do not promise persistence after Allocation replacement or node loss.
- Tunnel is the SDK and CLI capability; TunnelSession is its durable subordinate record.

If bytes must survive execution, the caller downloads them or publishes them through an explicit artifact-delivery contract. A random writable file does not become durable because it existed in a sandbox.

## SDK Sandbox

`Sandbox` is a first-class SDK experience, not a control-plane object:

```text
Sandbox
  = resolved Environment
  + owning Run
  + current Allocation
  + allocation-scoped operations
  + cancellation and cleanup
```

An SDK may create a temporary Environment, create and wait for the Run, expose the current Allocation ID, renew Tunnel sessions, and perform best-effort cleanup. It must preserve Run and Allocation identity, failure attribution, and deadlines instead of hiding them behind a second lifecycle.

Closing a Sandbox terminates or cancels its Run according to the SDK contract, revokes subordinate sessions, and performs bounded idempotent cleanup. Cleanup errors must not mask the original startup or execution failure.

## Results And Allocation-Local Outputs

Run result is durable control-plane metadata: terminal status, exit-code knowledge, failure classification, message, and usage. It does not make stdout, stderr, or sandbox files durable.

Stdout and stderr remain Allocation-local. When infrastructure cleanup begins, the control-plane transaction freezes `output_expires_at` at 15 minutes after that transition. Before deleting live logs, the node seals a bounded read-only snapshot; output reads survive runtime cleanup and node-process restart until that deadline, but not node-disk loss. The API delivers at most 64 MiB combined, with an explicit truncation signal; snapshot storage keeps at most 64 MiB plus one byte per stream to preserve existing cursors. Output-only access cannot execute processes or modify files. Ordinary writable files must be explicitly downloaded or archived before Allocation cleanup; durable publication belongs to the caller.

Axern does not define a generic public Artifact root object or imply an object-storage backend. Large stdout, files, and object bytes do not belong in PostgreSQL; PostgreSQL stores only Run result metadata.

## State Ownership

| State                                                                  | Authoritative owner               | Explicitly not authoritative               |
| ---------------------------------------------------------------------- | --------------------------------- | ------------------------------------------ |
| Principal, Namespace, Environment, Run, Allocation, Secret metadata    | controld / PostgreSQL             | gateway cache, node recovery state         |
| Allocation resource charges, access grants, TunnelSession, capability requirements, audit | controld / PostgreSQL | process-local queues or relay memory |
| ExecutionLease receipt deadline | axnoded Allocation recovery record, derived from controld heartbeat response | gateway access-grant cache or OCI metadata |
| Runtime, mount, network, cleanup, output, and node recovery state      | axnoded and its node-local stores | cross-node product truth                   |
| Image cache and read-only mount leases                                 | imagemgr / imagefsd               | Run status or writable workspace truth     |
| Process streams, PTY, terminal, SSH connections                        | node and gateway transient state  | durable results or long-term authorization |
| Delivered object bytes                                                 | explicit object-storage contract  | PostgreSQL and allocation-local files      |

Each fact has one authority. Caches and projections must identify their source, revision, owning Allocation where applicable, and invalidation condition; restart or partition must not promote them into a second source of truth.

Run and Allocation deliberately do not share facts:

| Fact | Sole durable owner |
| --- | --- |
| immutable execution config, labels, public lifecycle, cancellation, optional exit code, diagnostic, message, optimistic concurrency version, user timestamps | Run |
| Allocation ID, Run ownership, Node binding, infrastructure lifecycle, node-active and cleanup timestamps | Allocation |
| runtime/container existence, mount/network/cgroup cleanup progress | axnoded node-local state, converged into Allocation lifecycle |

## Objects Outside The Core

The following concepts must not enter the controld workload schema, public core API, or axnoded lifecycle state machine:

| Concept                                                     | Owner                                                                                       |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Service, Replica, Rollout, Route                            | External PaaS or an upper-layer application                                                 |
| Function, Invoke, worker dispatch                           | Run caller or an upper-layer queue/workflow system                                          |
| Agent Profile, model provider, prompt, budget               | Agent harness or training system                                                            |
| Evaluation, TaskSet, Episode, Verifier, Trajectory, Dataset | Axrun, Openbench, or another evaluation/training layer                                      |
| Persistent volume provisioning                              | External storage/workspace layer; core execution uses allocation-local storage, read-only image mounts, and explicit output delivery |
| Persistent Workspace                                        | Upper-layer logical concept; not a control-plane filesystem resource                        |
| Browser Dashboard or remote IDE                             | Separate product composed from process, file, terminal, SSH, and Tunnel capabilities        |
| Cluster, Region, cloud account, image build                 | Deployment infrastructure, Acklet, Kova, or another provider system                         |

Axrun may live in the same repository and integrate deeply through public SDKs. That does not authorize Axrun TaskSet, agent, verifier, trajectory, or rollout records to become Axern control-plane objects.

The package name `runtime/axnoded/internal/service` denotes an implementation service layer, not a product object, and does not require a mechanical rename.

## API And Persistence Rules

- Public creation is organized around Namespace, Environment, Run, Secret, and narrowly scoped subordinate operations.
- Only controld admission creates an Allocation. A public caller may inspect or target it but cannot bypass Run ownership.
- Every allocation data-plane operation names an Allocation ID and validates authority bound to that exact, never-reused identity.
- Protobuf messages, SDK types, CLI nouns, metrics, and database tables use the same domain meaning. Aliases must not create a second spelling for the same object.
- During pre-stable development, protobuf numbers and the initial local schema may be rebuilt as one coordinated change. Do not retain obsolete tables, fields, numbering holes, tombstones, dual reads, or compatibility guards.
- After an external contract becomes stable, compatibility is defined at the documented public object boundary. Internal tables, caches, metrics, or package names do not become public contracts by accident.
- CRUD, list, watch, event, and metric surfaces exist only for demonstrated access patterns. Do not mechanically clone all of them for every object.

## Lifecycle Invariants

Every implementation must preserve these invariants:

1. Environment input is immutable, verifiable, and rebuildable on another eligible Node.
2. Run is the only owner of user execution intent and terminal result.
3. Allocation IDs are globally unique and never reused; reports, operations, and cleanup for one ID cannot mutate another Allocation.
4. Allocation resource charge is not released before execution and required cleanup are confirmed complete.
5. ExecutionLease, interactive AllocationAccessGrant, SSH, and Tunnel authority ends with the owning Allocation. Output-only grants can read the bounded retained logs until `output_expires_at`; they never authorize execution.
6. Missing isolation, capability, network, mount, or resource enforcement fails closed.
7. Node-local files and stdout are not described as durable without explicit delivery.
8. Control-plane restart, node restart, and network partition cannot create two authoritative owners.
9. Cleanup is idempotent, owner-aware, and observable; unknown state is not reported as safely released.
10. Evaluation, retry policy, scoring, and training semantics remain outside the Run/Allocation state machine.

## Adding A New Object

A proposal for a new first-class object must answer all of the following before adding API or schema:

1. Why can this not be a field, subordinate record, allocation capability, or SDK helper?
2. Who creates, reads, mutates, terminates, and cleans it up?
3. What are its identity, state machine, authorization, quota, retention, and audit contracts?
4. How does it relate to Run, Allocation, Node, restart, and partition recovery?
5. Which tables, indexes, reconcilers, watches, metrics, and validation paths does it add?
6. Which real evaluation, training, or data-synthesis workload proves that cost is necessary?

The default is to avoid a new object. Update this document before introducing a parallel lifecycle into a public API or durable schema.
