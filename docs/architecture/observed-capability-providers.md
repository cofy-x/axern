# Capability Admission and Observation

Axern models capability with four deliberately separate values:

1. an immutable capability requirement in the Allocation specification;
2. the latest ordered Node observation;
3. the Allocation-to-Node binding and create decision;
4. a rebuildable runtime condition projection.

There is no durable capability-admission object, capability proof set, snapshot ID, per-capability generation, or condition revision stream.

## Facts and owners

| Value | Owner | Persistence | Purpose |
| --- | --- | --- | --- |
| Environment inputs | Environment | control database | User-selectable extension requirements and sandbox configuration |
| Capability requirements | Allocation | control database and node Allocation record | Immutable keys and contract-owned loss policy |
| Node observations | Node | latest `NodeSummary` plus durable `(node_instance_id, sequence)` fence | Current placement and node-create eligibility |
| Allocation binding | Allocation | control database; node control-plane binding | The unique Allocation ID is bound to one Node |
| Launch verification | Allocation on axnoded | node Allocation record | Runtime enforcement checked in the create-before-start window |
| Capability conditions | Allocation diagnostic projection | latest control database row only | Current operability or blocking reason; never lifecycle authority |
| Reconcile intent | Allocation on axnoded | node Allocation record | Crash-safe request to re-evaluate or finish fail-stop cleanup |

Run exposes the latest Allocation condition projection to users. It does not own or duplicate capability admission facts. Runtime, cgroup, egress, SSH, and Tunnel all use the same globally unique Allocation ID.

## Observation

Axnoded assigns each typed platform key to exactly one provider. Providers publish complete atomic batches for their owned keys. A provider error, panic, malformed batch, or expiry produces `UNKNOWN`; a successful stale sample is not kept available. The manager publishes an atomic `CapabilitySnapshot` with a node process identity, a monotonic sequence, collection time, and observations.

`(node_instance_id, sequence)` is the only observation ordering mechanism. Within one instance, sequence and collection time cannot move backwards and the owned key set cannot change. A restarted node uses a new instance identity and sequence starts again; a previously superseded instance cannot become current again. Exact same-sequence replay is idempotent only when the message is equal. This rejects duplicate, delayed, and out-of-order reports without inventing a snapshot digest or ID.

Observations contain state, provider, sample/expiry times, reason, and typed host evidence only when the inspected subject needs it:

- boot identity for boot-scoped kernel facts;
- boot plus mount identity for mount-scoped facts;
- boot, runsc binary digest, and runsc configuration digest for runtime facts.

Configuration and contract-derived facts carry no synthetic evidence. Digests identify inspected runtime content; they are not domain identity or ordering. Derived capability availability is evaluated recursively from the capability definitions and the current base observations. It is not frozen into a dependency proof.

Network and filestore health are refreshed and expire. Expired, absent, degraded, unavailable, and unknown observations fail placement and new node create closed. Recovery of ordinary health requires two distinct successful samples; distinctness is determined by increasing `observed_at`, not by a separate generation. Destructive runsc conformance requires one complete successful rerun after identity change.

## Admission

The shared capability contract derives workload requirements from the resolved sandbox: ports, network backend, egress policy, memory limit, writable rootfs, backing representation, and explicit extension requirements. Internal provider facts cannot be injected as workload requirements. The capability definition owns loss policy.

Controld evaluates the latest Node observation during candidate planning and again inside the resource-admission transaction after locking the Node record. That transaction creates the Allocation binding, immutable requirements, resource charge, and lifecycle create intent. It does not preserve a historical copy of the selected observation or a second admission header: the binding plus requirements and successful transaction are the decision.

Axnoded independently derives the requirements and compares them exactly with the request before any rootfs, mount, cgroup, egress, or runtime side effect. It then checks its current Node snapshot and persists the request digest plus immutable requirements as the first create side effect. The request digest is only the idempotency key for that Allocation create contract. An exact retry of an already active Allocation uses this durable contract and launch verification; it does not retroactively re-admit against a changed Node observation.

After runtime create and before workload start, axnoded verifies the applicable runtime/cgroup/egress controls and persists launch verification with the immutable enforcement manifest. These records are required for crash recovery; the effective OCI/runtime configuration is rebuilt from them and is not another domain fact.

## Conditions and reconcile

A condition set contains one condition per immutable requirement and a single `observed_at` timestamp. Conditions copy neither requirements nor observations. They cannot change Allocation/Run lifecycle, readiness, exit code, or primary message. Controld stores only the newest complete set. Older reports are ignored, equal-time exact replay is idempotent, and an equal-time conflicting payload is rejected. Reports for stopped, releasing, or released Allocations are ignored, so a late message cannot revive execution.

Node observation transitions enqueue one Allocation-scoped durable intent using the snapshot sequence. Multiple transitions merge to the newest sequence. One worker per Allocation evaluates the complete requirement set once, publishes one condition projection, and acknowledges that sequence. A bounded sharded audit schedules the same operation to cover a transition-delivery or local state-write failure; it performs only cheap runtime/kernel checks.

`ADMISSION_ONLY` changes running nothing. `DEGRADE` reports loss but preserves the sandbox. `FAIL_STOP` verifies allocation-specific enforcement at bounded delays; definitive loss, or inability to verify enforcement after the retry window, durably marks termination before cleanup starts. On restart axnoded resumes pending evaluation or termination from the Allocation record. Controld does not maintain a second capability queue and never races node-owned cleanup.

The lifecycle status outbox remains separate because terminal status crosses a process boundary and must survive crashes. It is not a capability cache.

## Diagnostics

Node Admin exposes the current Node snapshot and an Allocation's immutable requirements, latest conditions, and latest memory observation. Historical placement observations, memory admission snapshots, transition tables, proof digests, and reconcile backlogs are intentionally absent. Logs and metrics may describe bounded states and reason codes but never participate in admission.
