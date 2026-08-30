# Observed Capability Providers

Axern models node capability as `Observation -> Policy -> Enforcement`. A
capability is never a free-form node label and is never inferred from a
successful user sandbox.

## Terminology and sources of truth

This document is the canonical cross-subsystem contract for observed node
capabilities. The wire shape lives in
`sdk/proto/axern/control/capability/v1/capability.proto`; the platform catalog
and pure validation rules live in `lib/go/nodecapability`; axnoded's snapshot
manager lives in `runtime/axnoded/internal/nodecapability`.

Observed node capabilities are not either of these unrelated concepts:

- a narrow Go interface sometimes called a capability at a consumer boundary;
- sandboxd operation discovery exposed through `NodeSandbox.CapabilityStatus`.

Runtime handlers also expose static handler features and resource requirements.
Those declarations select local runtime wiring; they cannot publish observed
platform evidence or satisfy placement by themselves.

## Observation

Axnoded's capability manager assigns every typed key to exactly one provider.
Every provider has an independent scheduler and atomically publishes one
complete `ObservationBatch` for its owned keys. Network and mount health cannot
be delayed by the serial runtime-conformance worker. A provider error, panic,
malformed batch, or expiry publishes `UNKNOWN` for all affected owned keys; an
old successful batch is never retained past its validity window.

The manager aggregates the latest batches under one lock and publishes one
atomic `CapabilitySnapshot`. Atomic publication means readers see a coherent
generation; it does not mean every provider sampled the host simultaneously.
Each observation preserves its real sample completion time, while
`snapshot.collected_at` is the aggregation publication time. Inventory reads
only this complete snapshot and reports collector health separately. The node
remains `WARMING` until every expected provider has produced an initial result.
After that, one unavailable capability blocks only workloads that require it.

Snapshot publication is independent from ordered transition delivery. A single
delivery worker invokes metrics and transition subscribers in generation order,
so an allocation reconcile enqueue cannot hold the publication lock and delay
five-second network or mount refresh. Subscriber panics are isolated from later
subscribers and publications. The delivered context is detached from the probe
caller after publication; cancellation of a completed probe cannot suppress the
durable reconcile enqueue. Subscribers must remain bounded and persist work
instead of running allocation verification inline. Periodic allocation audits
remain a convergence safety net for shutdown races or a temporary node-state
write failure, but they are deliberately cheap: control-file reads,
source/runtime identity, and PID membership only. A stable allocation hash
spreads one full pass over ten minutes, with at most four node-wide reconcile
workers. Real OOM and quota-fill probes are forbidden from this path.
Consecutive publications with no semantic transition coalesce to
the newest pending snapshot while a subscriber is busy; transition-bearing
generations remain ordered and are never coalesced.

Platform keys are a closed proto enum. The central catalog owns each platform
key's provider, evidence identity kind, freshness policy, workload audience,
loss policy, verifier, and bounded direct dependency set. Startup rejects incomplete
enum coverage, duplicate ownership, missing dependencies, cycles, and invalid
fact/derived boundaries. Production wiring performs a second coverage check:
every catalog key must be owned exactly once by a registered provider, and the
registered owner must equal the catalog owner. A catalog declaration without a
live producer is a startup error rather than a permanently missing fact.
Config may publish only exact-match extensions named `<dns-domain>/<name>`;
Axern-owned domains are reserved. Inputs must already have no surrounding
whitespace; DNS domains are canonicalized to lowercase, while the name suffix
and extension value are preserved byte-for-byte for exact matching. Nodes and
workloads may each carry at most 64 extensions and each value is bounded to 256
bytes. Duplicate or nil requirements are invalid rather than silently removed.
Evidence identity and freshness are orthogonal. A typed identity `oneof` binds
a fact to config digest, boot ID, boot plus mount identity, or the
boot/runtime-binary/runtime-config tuple. `valid_until` is an independent
expiry, so a mount-scoped or runtime-scoped fact can also be short-lived. A
stable `evidence_id` names the validated subject; a stable `observation_id`
names one observation of that subject. Refresh never turns one into the other.
`observation_id` is the canonical digest of the proof-persisted key, provider,
sample time, expiry, and evidence. Any receiver can recompute it; opaque or
tampered observation IDs are rejected without trusting the sender.
Expired facts fail closed even when the enclosing `NodeSummary` is fresh.
Boot identities are canonical lowercase UUIDs. Configuration, runtime, catalog,
and proof-set digests use canonical lowercase `sha256:<hex>` encoding.
Filestore mount identity is an opaque, bounded mount-instance token: consumers
compare it for equality and never parse filesystem details from it. Its
canonical digest covers the kernel mount/parent IDs, device, mount root and
path, filesystem/source, propagation fields, and normalized mount and superblock
options. A read-only remount or project-quota option change therefore invalidates
the fact even when the mount ID and source remain unchanged.
Derived evidence binds both the catalog digest and a canonical digest of every
dependency key plus evidence identity. It therefore changes when policy or a
validated subject changes, but not merely because the same subject was sampled
again.

The providers are:

- config: extension facts;
- host: a boot-scoped cgroup v2 memory-controller enforcement probe;
- network health: bridge/iptables or BPF dataplane, port forwarding, and the
  egressd DNS/strict policy self-tests;
- filestore: mount identity, OverlayFS upper, XFS project quota, and real EROFS
  compatibility;
- runtime conformance: local, registry-independent runc and runsc sandboxes;
- derived policy: runtime-specific memory and ephemeral-storage hard limits,
  DNS policy enforcement, and strict egress enforcement.

The catalog currently owns this platform set:

| Capability group | Provider and scope | Loss policy | Dependency rule |
| --- | --- | --- | --- |
| Port forwarding, bridge, BPF network | network health, refreshable | `DEGRADE` | direct observed health |
| Cgroup v2 memory controller | host cgroup, boot | `ADMISSION_ONLY` | direct enforcement probe |
| Runc/runsc memory hard limit | derived, refreshable | `FAIL_STOP` | cgroup fact plus matching runtime self-test |
| Filestore OverlayFS upper, XFS project quota | filestore, mount | `ADMISSION_ONLY` | direct mount-scoped probe |
| Runc ephemeral-storage hard limit | derived, refreshable | `FAIL_STOP` | OverlayFS upper, XFS quota, and runc self-test |
| Runsc ephemeral-storage hard limit | derived, refreshable | `FAIL_STOP` | OverlayFS upper and runsc self-test |
| EROFS lower compatibility | EROFS probe, mount | `ADMISSION_ONLY` | real fixture probe |
| Runtime memory and ephemeral self-test facts | matching runtime self-test, runtime | `ADMISSION_ONLY` | internal dependencies, not workload requirements |
| Egressd DNS and strict self-test facts | network health, refreshable | `ADMISSION_ONLY` | internal dependencies, not workload requirements |
| DNS policy enforcement | derived, refreshable | `FAIL_STOP` | egressd DNS policy self-test |
| Strict egress enforcement | derived, refreshable | `FAIL_STOP` | egressd DNS and strict policy self-tests |

Extensions are config-static, config-owned, and always `ADMISSION_ONLY`.
The extension config digest is independent from the network config digest.
Network evidence hashes the complete normalized `NetworkConfig`, including
semantic set ordering and effective defaults; changing extensions cannot
manufacture a network transition, while changing any behaviorally relevant
network field must change network evidence. A network sample publishes every
network-owned platform key atomically: the configured backend reports its live
result, and unselected backends report explicit `UNAVAILABLE/DISABLED`. Missing
keys therefore mean a malformed provider batch, not an implicit configuration.

Network and filestore health are sampled every five seconds and expire after
15 seconds. Runtime identity is checked on the health cadence. The expensive
conformance sandbox runs only for initial certification, after a runtime/config
identity change, and during explicit deployment qualification. A failed probe
retries with bounded backoff. A successful conformance fact is identity-scoped
and does not expire merely because time passed. A failed
boot cgroup probe retries with exponential backoff. Static config facts bind
their provider-specific digest and have no TTL. Recovery from a base-provider
error, panic, malformed output, or expiry requires two independent successes
at least five seconds apart. A failed destructive runtime conformance needs one
complete successful rerun, not two destructive reruns. Derived capabilities add no second debounce: they
become available when every dependency has already completed its own recovery
policy, because recomputing the same pure expression is not independent host
evidence.

Runtime conformance providers share one global serial lane across runc, runsc,
memory, and ephemeral-storage probes. The resource manager independently
enforces the same single-owner rule, so a scheduler regression cannot create a
second destructive certification sandbox. Certification cgroups live under a
reserved `conformance` sibling of the configured sandbox domain, with an
aggregate 256 MiB `memory.max`, zero swap, and group OOM. Their reservation and
cleanup debt are charged to `memory_system_reserve_bytes`; they are excluded
from workload slots, sandbox `memory.current`, and workload memory commitment.
Admission checks both current and committed system-reserve headroom before a
probe starts. Memory and ephemeral-storage enforcement use separate self-test
sandboxes and observations, so a storage failure cannot suppress memory evidence
and a memory-limit probe failure cannot rewrite storage evidence. The shared
bounded certification domain remains a required node-safety prerequisite for
both. Each self-test is limited to 60 seconds, reruns only
after runtime/config identity changes or a prior failure, and retries failures
with exponential backoff capped at five minutes. Self-test
cleanup is part of success and remains inside the 60-second probe deadline, with
up to 30 seconds reserved for runtime teardown. Each runtime/kind pair uses one
deterministic, reserved allocation identity: an interrupted probe is reconciled
before retry instead of creating a new bundle, projection, or reservation.
Cleanup verifies that the bundle and runtime-owned storage paths are absent.
The node memory collector validates the reserved domain, includes its inode in
the capacity identity, and publishes bounded current, commitment, cleanup-debt,
and provider duration/result metrics. Removing or weakening the domain invalidates
the memory budget instead of silently moving certification back into workload
capacity.
Recovery hysteresis is applied by the capability manager to base observations
before derived providers run, so a derived capability can never advertise an
unconfirmed dependency recovery. Observation time is the real probe completion
time, not the scheduler wake-up time; retry and conformance periods are also
measured from completion.
When a certified runtime identity changes, the scheduler first publishes
`UNKNOWN/IDENTITY_CHANGED`; the serial worker performs the expensive
conformance probe on its next run. The prior evidence is never kept available
while that probe is running. Runtime binary digests are cached by kernel file
identity. On Linux the cache key includes device, inode, mode, size, mtime, and
ctime, so an in-place rewrite that preserves size and mtime still invalidates
the certified runtime subject.

## Policy

The shared catalog is the only owner of platform loss policy. Workload-facing
requirements are automatically derived from ports, network mode, egress
policy, memory limit, writable rootfs/runtime, and EROFS mount representation.
Users can request only extension capabilities. DNS-only and strict egress
requirements are mutually exclusive. Isolated or explicit strict deny-all does
not require egressd because the existing isolated dataplane can prove that no
egress path exists.

Only an `AVAILABLE` observation whose snapshot, validity period, evidence
scope, and derived dependency evidence remain valid is eligible for placement.
`DEGRADED`, `UNAVAILABLE`, `UNKNOWN`, missing, expired, or identity-mismatched
observations all reject new work.

## Enforcement

Controld performs the same typed eligibility evaluation during candidate
planning and inside the PostgreSQL admission transaction after locking node
rows. Representation- and backend-specific requirements, including EROFS and
the observed bridge/BPF dataplane, are re-derived from that locked summary.
Each planning generation uses one captured time for eligibility and evidence
resolution; admission uses its own transaction time and re-evaluates all facts.
Allocation, resource reservation, capability dependency manifest, and
placement evidence commit atomically.

Axnoded independently derives requirements with the same pure catalog function
used by controld. Before any allocation side effect it derives request-static
requirements. After acquiring the image mount lease, but before bundle,
filestore, cgroup, or runtime side effects, it adds requirements from the actual
rootfs backing. The supplied dependency keys must exactly match this locally
derived set: missing keys, duplicates, extra internal facts, and malformed or
stale proofs are rejected. Node-owned conformance uses an in-process-only
harness marker and cannot be invoked through lifecycle RPC to bypass this gate.

The proof layers are deliberately distinct:

- placement proof records the controld transaction's selected snapshot and
  observations;
- create proof records the current observations axnoded actually admitted and
  any explicit evidence replacement;
- runtime enforcement proof is the immutable launch manifest plus live
  kernel/runtime verification.

Before allocation materialization, axnoded atomically persists the locally
admitted dependency proofs, a canonical digest of the behaviorally relevant
create request, and a complete healthy condition set at revision 1. Trace IDs
and replaceable placement observation proofs do not affect that digest; runtime,
rootfs, resources, command, mounts, volumes, secrets, and extension requirements
do. A same-attempt retry of an active allocation must match the durable digest.
Capability admission, runtime creation, post-create verification, replay, and
Delete share one allocation lifecycle lock. Once launch verification exists, an
exact retry returns the immutable admitted dependency and current condition
projection without re-admitting current node observations or rewriting create
proof. Capability loss is handled by the allocation-specific reconciliation
path, not by changing the historical result of Create.
No runtime, filestore, cgroup, bundle, or rootfs side effect may precede that
write. After runtime creation and allocation-specific enforcement verification,
it atomically replaces both projections with the post-create proof and revision
2 condition set. Controld commits the proof rows and a separate immutable
admission header in one transaction, so even an allocation with no dependencies
has an unambiguous create-proof record. A normal managed allocation is not
recoverable if either half of its node-local admission record is missing;
recovery fails closed instead of
reconstructing a partial proof from runtime state.

After runtime create, axnoded reads actual `memory.max` and host cgroup
membership. Runc must reconcile the runtime state init PID with the immutable
pid-file PID before accepting membership. Runsc verifies Sentry and gofer
roles, executable identity, and cgroup membership; guest workload memory is
accounted through Sentry rather than a fictional guest host PID. Runc storage
verification reads project ID and kernel quota and checks the OverlayFS and
filestore identity. Runsc verifies the immutable `root:dir=...,size=...`
envelope, runtime process identity, state, backing path plus device/inode
identity, and filestore mount identity. The authoritative create proof and full
structured condition set are returned to controld.

Memory hard-limit evidence requires cgroup v2. There is no cgroup v1
`memory.limit_in_bytes` compatibility path and no dev-mode resource-dropping
fallback for a declared memory limit: inability to write, read back, or prove
process membership fails the create or conformance probe closed.

An evidence or state transition immediately blocks new node-local creates.
Running allocations are then verified individually:

- `ADMISSION_ONLY` affects only new work;
- `DEGRADE` keeps the sandbox after network/port state verification and reports
  a structured condition;
- `FAIL_STOP` terminates the sandbox after definitive enforcement loss, or
  after enforcement cannot be proven at 0, 2, and 5 seconds. Allocation
  verification distinguishes `VERIFIED`, definitive `LOST`, and
  `INCONCLUSIVE`; a node-level observation state is never itself treated as
  proof that a particular allocation lost enforcement.
  All pending hard capabilities are sampled in each retry round. A definitive
  loss ends the batch in that round and cannot wait behind another capability's
  inconclusive retry schedule; unrelated unexhausted probes are not falsely
  reported as additional enforcement losses.
Even when the allocation verifier succeeds, an unavailable node observation
keeps the condition `DEGRADED`; only current node evidence plus successful
allocation verification can produce `HEALTHY`.

Axnoded persists dependency proofs, the immutable enforcement manifest, a full
condition set with monotonic revision, and an allocation-scoped durable
reconcile queue. A transition only merges the latest key generation; it never
runs a verifier inline. At most one reconcile/termination worker owns an
allocation, so simultaneous capability losses aggregate into one ordered
fail-stop workflow. New generations arriving during verification are processed
in the next loop and are not lost. A condition persistence or queue-ack failure
leaves the generation pending and retries it; it is never logged and discarded.
Event transitions enqueue immediate work. The bounded, sharded audit covers
every `DEGRADE` and `FAIL_STOP` dependency with cheap control/identity/PID
verification as a durable safety net for a transition that could not be
enqueued during local state-store failure. Fail-stop cleanup is detached from
the request context and remains
durable until runtime deletion and resource cleanup converge. Axnoded is the
single owner of allocation termination for capability loss; controld's durable
reconciler polls and retries reconciliation but never issues a competing delete
for the same failure.

Capability conditions never own allocation lifecycle. A condition set is a
complete exact-key projection whose monotonic revision is fenced by allocation
attempt. Condition reports can replace it only at a newer revision for the
current attempt; they cannot change status, readiness, exit code, Run/Service
status, or the primary lifecycle message. Normal delete/exit reporting owns
those fields. An exact replay of the same attempt and revision is accepted only
when its canonical protobuf SHA-256 payload digest is identical; a different
payload at the same revision is rejected as equivocation. Runtime reconciliation
may replace only the condition projection and must prove that its dependencies
exactly equal the durable create proof. It cannot rewrite placement or
create-time admitted evidence.

## Persistence and diagnostics

The latest `NodeSummary` keeps the complete snapshot. PostgreSQL independently
stores idempotent node transitions and normalized allocation dependency,
condition-set, condition, and reconcile-key rows. The `(node_id,
capability_key_id)` dependency index finds affected allocations directly; node
reporting never scans every active allocation JSON document. `ADMISSION_ONLY`
dependencies are excluded from the runtime reconcile queue because their loss
policy ends at admission; `DEGRADE` and `FAIL_STOP` dependencies are queued for
allocation-specific verification. The node report
transaction writes the new summary, transitions, and affected queue items
before the in-process registry is updated.

Resource state has exactly three owners. Controld owns scheduling reservations;
axnoded owns node-local commitments and cleanup debt; the kernel/runtime owns
actual usage and enforcement. Admission uses the greater of the locked database
reservation and reported local commitment to close lifecycle races. That
calculation is not persisted as a third ledger, and diagnostic inventory totals
never become an admission authority.

Node-instance history is also durable. Sequence and collection time cannot
move backwards within an instance, snapshot IDs cannot be reused, and a
superseded instance cannot become active again. This fences delayed reports
from an older axnoded process after a node restart.

Axnoded and controld use the same effective-transition evaluator. An absent
observation becomes `UNKNOWN/DEPENDENCY_UNAVAILABLE`; a raw `AVAILABLE`
observation whose freshness or dependency proof has expired becomes
`UNKNOWN/EXPIRED`. State, evidence identity, or bounded reason-code changes
create history, while timestamp-only refresh does not.

The Node Admin API exposes summary-only node lists plus dedicated snapshot,
transition, reconcile-backlog, and allocation capability diagnostic methods.
Metrics use bounded enum labels for platform capability, provider, state,
reason, runtime, and result. Extension observations are aggregated into bounded
counts; extension names, values, evidence IDs, and free-form diagnostics are
never metric labels.

This contract requires coordinated controld, axnoded, proto, SDK, and CLI
deployment. Mixed versions are unsupported; development databases and node
state are rebuilt during rollout.

Public memory is a total sandbox memcg budget as finalized by issue #43.
Capability evidence proves the runtime can enforce that host boundary; it does
not add a runtime overhead reservation or reinterpret guest-usable headroom.
Anonymous memory, shmem, kernel memory, EROFS lower page cache, file-backed
overlay page cache, dirty pages, and writeback are usage attribution within the
same limit. Node-local daemons remain outside that boundary and are covered by
the independently qualified node system reserve.
