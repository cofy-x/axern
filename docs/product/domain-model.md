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
| `Node` | Administrative identity for one unit of execution supply, with an audited active/retired lifecycle | `controld`; observations originate from `axnoded` |

Catalog templates and Agent Bundles are platform-managed, read-only reference data. They have stable identifiers and versions but are not user-created workload lifecycles or a marketplace model.

### Namespace

A Namespace scopes Environments, Runs, Secrets, quota policy, and retained metadata. Namespace deletion must reject live operational state, including an active Run, Allocation, Reservation, TunnelSession, or Secret. Historical terminal Run records may retain the namespace string without keeping the Namespace alive.

Namespace is not an alias for a Kubernetes namespace, cluster, region, cloud account, project-management system, or billing account.

### Environment

An Environment answers what reproducible input a Run starts from. Its source is a versioned template or an image resolved to immutable identity before execution. Read-only image mounts and normalized execution policy may be part of the resolved input; allocation-local writable files are not.

Environment meaning is immutable. A source or policy change creates a new Environment. Run creation freezes the resolved input required to interpret and rebuild that Run; deleting an Environment must not silently change historical Run meaning.

An Environment does not contain replicas, rollout, service discovery, readiness policy, an Agent Profile, or a persistent Volume declaration.

### Run

A Run is the smallest complete unit of execution visible to a user or SDK. It owns one immutable execution request, one logical Allocation, cancellation, terminal result, failure classification, and any explicit output references.

The stable state projection is:

```text
QUEUED -> PLACED -> STARTING -> RUNNING
   |         |           |          |
   +---------+-----------+----------+-> CANCELLED
                                    +-> SUCCEEDED
                                    +-> FAILED
```

These are the public Run states. Allocation has its own concrete reservation, binding, execution, releasing, and released states; internal detail must project unambiguously to the owning Run. Run has no replica convergence, rolling update, readiness, degraded-service, or desired-state controller.

One Run owns one logical Allocation. Allocation `attempt` fences retries of the same node lifecycle operation; it is not a promise of exactly-once external side effects. An infrastructure failure terminates the Run. A harness that wants another episode creates another Run instead of relying on hidden replay.

### Secret

Secret plaintext is accepted only by authorized creation or use paths. Normal read APIs return metadata, never plaintext. Run and Environment state store a reference and necessary version identity, not secret content. Plaintext must not enter logs, events, metrics, diagnostics, or artifacts.

Deletion, rotation, and missing-reference behavior must be explicit. Axern does not silently substitute a same-named newer value for an already frozen Run.

### Principal, Credential, And RoleBinding

Gatewayd authenticates an external credential; controld resolves it to a durable Principal and applies RoleBindings. A certificate fingerprint or token is authentication material, not permanent business identity.

Platform workload identities for gatewayd, axnoded, and tunneld remain distinct from user Principals. Internal services must not reuse an external client credential when calling controld.

### Node

Node identity is durable and independent from heartbeat freshness. Retirement is an audited, irreversible administrative transition and requires execution, lease, tunnel, retry, reservation, and cleanup blockers to converge. A replacement host uses a new Node ID; a stale host must not regain authority by reusing a retired identity.

## Durable Subordinate Execution Records

These records need durable identity and state, but users cannot create them independently from their owner.

| Object                 | Direct owner            | Purpose                                                              |
| ---------------------- | ----------------------- | -------------------------------------------------------------------- |
| `Allocation`           | Run                     | Concrete node placement and attempt-fenced execution identity        |
| `Reservation`          | Allocation              | Committed namespace and node resource usage                          |
| `ExecutionLease`       | Allocation attempt      | Short-lived internal gateway authority for the selected node         |
| `TunnelSession`        | Allocation attempt      | Revocable reverse-TCP session and relay convergence state            |
| `CapabilityDependency` | Allocation              | Capability and loss-policy proof frozen at admission                 |
| `CapabilityCondition`  | Allocation attempt      | Current attempt-fenced satisfaction or enforcement state             |
| `QuotaPolicy`          | Namespace               | Optional CPU, memory, and ephemeral-storage admission ceilings       |
| `AuditEvent`           | Administrative mutation | Actor, reason, target, and result for security-sensitive writes      |
| `OperationalEvent`     | Owning domain           | Bounded lifecycle or rejection history, distinct from operator audit |

### Allocation

Allocation is the only concrete execution target. It binds a Run to a Node, attempt, resolved environment, resources, capability evidence, runtime status, and cleanup state. Terminal, SSH, Tunnel, process, file, and administrative lifecycle operations select an Allocation explicitly.

Allocation cannot exist as a durable orphan or an independently created public workload. A Run is not fully released until runtime, network, mounts, leases, tunnels, reservations, and node recovery ownership have converged.

### Reservation

Reservation is a transactional admission ledger, not a user-managed resource. It commits with Allocation admission and accounts for CPU, sandbox memory, ephemeral storage, and runtime slots at the Namespace and Node boundaries.

A failed or timed-out RPC is not evidence that resources are free. Reservation release follows confirmed terminal execution and required node cleanup.

### ExecutionLease

ExecutionLease is short-lived internal authority bound to Allocation ID, Node ID, attempt, operation type, expiry, and revocation state. PostgreSQL stores a token hash; plaintext is returned only on the controlled issuance path.

Public SDKs do not persist or replay ExecutionLeases. Gateway retries may refresh authority only before the selected node accepts an operation. A stale attempt cannot submit input, output, status, or cleanup for a newer attempt.

### TunnelSession

TunnelSession is a durable, subordinate session because relay pairing, renewal, revocation, expiry, restart convergence, and traffic accounting outlive one connection. It is always bound to one Allocation and attempt.

TunnelSession does not accept Service identity, choose a replica, or recreate a `/svc` route. Allocation termination, attempt change, authorization revocation, or TTL expiry invalidates the session.

### Capability Evidence

Node capability observations are typed, time-bounded platform facts. Allocation dependencies freeze the exact requirement and loss policy admitted for one execution. Conditions are complete, revisioned, attempt-fenced projections of runtime satisfaction.

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

## Results And Durable Outputs

Run result is part of the Run contract: terminal status, exit-code knowledge, failure classification, message, usage, and explicit output references.

Axern does not currently define a generic public Artifact root object. A future artifact API may expose immutable, content-identified output subordinate to a Run and Namespace only when ownership, upload commit, integrity, retention, authorization, and garbage collection are defined together. It must not expose object storage as a mutable POSIX Volume.

Large stdout, files, and object bytes do not belong in PostgreSQL. PostgreSQL stores result metadata and references; node output and object storage retain their own explicit lifetime contracts.

## State Ownership

| State                                                                  | Authoritative owner               | Explicitly not authoritative               |
| ---------------------------------------------------------------------- | --------------------------------- | ------------------------------------------ |
| Principal, Namespace, Environment, Run, Allocation, Secret metadata    | controld / PostgreSQL             | gateway cache, node recovery state         |
| Reservation, ExecutionLease, TunnelSession, capability evidence, audit | controld / PostgreSQL             | process-local queues or relay memory       |
| Runtime, mount, network, cleanup, output, and node recovery state      | axnoded and its node-local stores | cross-node product truth                   |
| Image cache and read-only mount leases                                 | imagemgr / imagefsd               | Run status or writable workspace truth     |
| Process streams, PTY, terminal, SSH connections                        | node and gateway transient state  | durable results or long-term authorization |
| Delivered object bytes                                                 | explicit object-storage contract  | PostgreSQL and allocation-local files      |

Each fact has one authority. Caches and projections must identify their source, revision, attempt, and invalidation condition; restart or partition must not promote them into a second source of truth.

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
- Every allocation data-plane operation names an Allocation ID. Operations that can race replacement or retry also validate attempt-bound authority.
- Protobuf messages, SDK types, CLI nouns, metrics, and database tables use the same domain meaning. Aliases must not create a second spelling for the same object.
- During pre-stable development, protobuf numbers and the initial local schema may be rebuilt as one coordinated change. Do not retain obsolete tables, fields, numbering holes, tombstones, dual reads, or compatibility guards.
- After an external contract becomes stable, compatibility is defined at the documented public object boundary. Internal tables, caches, metrics, or package names do not become public contracts by accident.
- CRUD, list, watch, event, and metric surfaces exist only for demonstrated access patterns. Do not mechanically clone all of them for every object.

## Lifecycle Invariants

Every implementation must preserve these invariants:

1. Environment input is immutable, verifiable, and rebuildable on another eligible Node.
2. Run is the only owner of user execution intent and terminal result.
3. Allocation has one valid attempt at a time; stale attempts cannot report, execute, or clean up newer state.
4. Reservation is not released before execution and required cleanup are confirmed complete.
5. ExecutionLease, SSH, and Tunnel authority ends with the Allocation attempt.
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
