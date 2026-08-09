# Observed Capability Providers

Axern models node capability as `Observation -> Policy -> Enforcement`. A
capability is never a free-form node label and is never inferred from a
successful user sandbox.

## Observation

Axnoded's capability manager assigns every typed key to exactly one provider.
Each refresh publishes one atomic `CapabilitySnapshot`; a provider error
produces `UNKNOWN` for every missing expected key and is not reclassified as
an inventory collector failure. Only an error that prevents construction or
validation of the complete snapshot fails collection. The node remains `WARMING`
until all providers have produced an initial result. Individual unavailable
capabilities do not make unrelated workloads unschedulable.

Platform keys are a closed proto enum. The central catalog owns each platform
key's provider, validity scope, loss policy, and derived dependency graph;
snapshots with a mismatched owner, scope, or dependency set are rejected.
Config may publish only exact-match extensions named `<dns-domain>/<name>`;
Axern-owned domains are reserved. DNS domains are canonicalized to lowercase,
while extension values are preserved byte-for-byte for exact matching.
Provider evidence is scoped to config digest, boot ID, mount identity, or the
boot/runtime/config identity tuple. Refreshable network and derived facts carry
an expiry and fail closed when stale.

The initial providers are:

- config: extension facts;
- host: cgroup v2 memory controller and delegation;
- network health: bridge/iptables or BPF dataplane and port forwarding;
- filestore: mount identity, OverlayFS upper, XFS project quota, and real EROFS
  compatibility;
- runtime conformance: local, registry-independent runc and runsc sandboxes;
- derived policy: runtime-specific memory and ephemeral-storage hard limits.

Runtime conformance is serialized, limited to 60 seconds, refreshed every 15
minutes or immediately after runtime/config identity changes, and retries
failures with exponential backoff capped at five minutes. Recovery requires two
distinct successful full probes at least five seconds apart. Self-test cleanup
is part of success. Recovery hysteresis is applied to base observations before
derived providers run, so a derived capability can never advertise an
unconfirmed dependency recovery from the same snapshot generation.

## Policy

The shared catalog is the only owner of platform loss policy. Workload-facing
requirements are automatically derived from ports, network mode, memory limit,
writable rootfs/runtime, and EROFS mount representation. Users can request only
extension capabilities.

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

Before any allocation side effect, axnoded verifies the selected dependencies
against its current snapshot and persists the dependency manifest. After
runtime create it performs runtime-specific verification of cgroup PID
attribution and ephemeral hard-limit backing. The authoritative admitted
dependency set, including selected evidence, dependency evidence, loss policy,
and any evidence replacement, is returned to controld and persisted with
structured conditions.

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

Delete and cleanup continue until confirmed. The separate durable capability
reconcile queue survives controld restarts and missed node reports; it never
overwrites create/delete lifecycle work.

## Persistence and diagnostics

The latest `NodeSummary` keeps the complete snapshot. PostgreSQL independently
stores idempotent node transitions, allocation dependencies, placement and
create-time evidence, conditions, and capability reconcile work. The node
report transaction writes the new summary, transitions, and affected queue
items before the in-process registry is updated.

Node-instance history is also durable. Sequence and collection time cannot
move backwards within an instance, snapshot IDs cannot be reused, and a
superseded instance cannot become active again. This fences delayed reports
from an older axnoded process after a node restart.

The Node Admin API exposes summary-only node lists plus dedicated snapshot,
transition, reconcile-backlog, and allocation capability diagnostic methods.
Metrics use bounded enum labels for platform capability, provider, state,
reason, runtime, and result; extension names and free-form diagnostics are not
metric labels.

This contract requires coordinated controld, axnoded, proto, SDK, and CLI
deployment. Mixed versions are unsupported; development databases and node
state are rebuilt during rollout.
