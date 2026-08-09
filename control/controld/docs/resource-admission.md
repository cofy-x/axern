# Resource Admission

This document describes the control-plane resource admission path for workload
requests. It complements the namespace quota design by covering the full path
from placement prefiltering to Postgres reservation checks, diagnostics, and
metrics.

## Core Model

Workload `resources.requests` are the admission and reservation contract.
Workload `resources.limits` are runtime cgroup ceilings enforced by `axnoded`;
they are not quota usage and do not increase control-plane capacity.

CPU, memory, and runtime instance slots have distinct admission semantics:

| Resource | Admission behavior |
| --- | --- |
| CPU | Node allocatable CPU can be overcommitted through the global `-resource-cpu-overcommit-ratio` policy. |
| Memory | Memory remains strict; effective memory allocatable equals node reported allocatable memory. |
| Runtime slots | One workload reservation consumes one slot. Axnoded reports the aggregate node-owned slot capacity after applying enabled resource-pool constraints. |

Namespace quota is a namespace cap on requests. Node admission is a node cap on
requests after applying the global node policy. CPU overcommit never multiplies
namespace quota.

## Admission Sequence

Admission has two capacity gates:

1. Placement prefilters eligible nodes using the same resource policy as
   reservation admission and emits a request-scoped candidate plan. This gives
   fast eligibility diagnostics while retaining locality and warm-path
   preferences for final admission.
2. Postgres reservation admission locks namespace quota and candidate nodes,
   re-sums active reservations, evaluates namespace quota, CPU, memory, and
   runtime-slot capacity,
   refreshes candidate load using reservations not yet reflected by node
   summaries, and writes the workload reservation in one transaction.

The transaction check is authoritative. Placement is an optimization and a
source of candidate diagnostics, not the durable reservation boundary.

Runtime-slot admission uses the strongest current occupancy signal:

```text
active    = len(axnoded.active_allocation_ids)
pool_used = runtime_slots.using
occupied  = max(transactional_reservations, active, pool_used)
capacity  = runtime_slots.capacity - runtime_slots.unavailable
```

This counts each normal allocation once while reservation, runtime startup, and
node status reports move asynchronously. Resource-release failures retain the
durable allocation reservation until cleanup succeeds. Slots that cannot be
owned safely after a local creation/rollback failure are folded into
`runtime_slots.unavailable`; admission never infers the aggregate contract from
cgroup or interface implementation details.

`runtime_slots.idle` is the number of aggregate slots whose enabled node-local
resources are already materialized and can be reused immediately. Placement may
use it as a warm-start preference, but admission depends only on aggregate
capacity, unavailable slots, and occupancy. A missing `runtime_slots` contract
is invalid and fails admission closed.

## Structured Diagnostics

Resource admission errors use gRPC `ErrorInfo` details with domain:

```text
axern.control.resource_admission
```

Current reasons:

| Reason | Meaning | Typical code |
| --- | --- | --- |
| `NAMESPACE_QUOTA_EXCEEDED` | Namespace request quota would be exceeded. | `ResourceExhausted` |
| `NODE_RESERVATION_CAPACITY_EXHAUSTED` | No selected candidate has remaining CPU, memory, or runtime-slot reservation capacity after transaction recheck. | `ResourceExhausted` |
| `PLACEMENT_CAPACITY_EXHAUSTED` | Placement rejected candidates for CPU or memory capacity. | `FailedPrecondition` |
| `NODE_SELECTION_ERROR` | Placement found no eligible node for non-capacity reasons such as runtime, selector, readiness, or capability mismatch. | `FailedPrecondition` |

Human-readable error messages remain stable enough for operators, but workload
state, CLI rendering, dashboard DTOs, and future clients should prefer
`axern.control.common.v1.WorkloadDiagnosticCode` when it is available. Service
and run views expose that shared diagnostic code directly. Service
degradation maps quota, node reservation, and placement capacity failures to
`WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED`; non-capacity placement failures map
to node selection.

## Observability

Resource admission metrics are layered:

| Metric | Scope |
| --- | --- |
| `axern.controld_resource_admission_total{namespace,scope,result,reason}` | Shared resource admission decisions for namespace quota and node reservation. |
| `axern.controld_resource_admission_stage_duration_seconds{owner_type,stage,result,error_class}` | Durable namespace lock, candidate lock, reservation load, selection, and total admission latency. |
| `axern.controld_service_allocation_queue_duration_seconds{path,stage,result,error_class}` | Durable allocation queue claim-store, due-lag, eligible claim-wait, dispatcher-wait, and total latency. |
| `axern.controld_service_transaction_stage_duration_seconds{stage,result,error_class}` | Service transaction pool acquisition, body, commit, and total latency. |
| `axern.controld_postgres_pool_connections{state}` | Current controld Postgres pool maximum, total, acquired, and idle connections. |
| `axern.controld_quota_admission_total{namespace,result,reason}` | Namespace quota compatibility/detail metric. |
| `axern.controld_placement_selection_total{operation,result,mount_type}` | Placement selection attempts. |
| `axern.controld_placement_rejection_total{operation,result,reason}` | Placement candidate rejection reasons. |
| `axern.controld_namespace_resource_current{namespace,resource,state}` | Current namespace quota limits, reserved usage, and available capacity. |
| `axern.controld_node_resource_current{resource,state}` | Current node CPU, memory, ephemeral-storage, and runtime-slot capacity, reservation, and policy-derived resource state. |

Allocation queue stages have distinct meanings:

| Stage | Interval |
| --- | --- |
| `claim_store` | Time spent claiming one durable batch from Postgres. |
| `due_lag` | Time from `next_run_at` until the claim completes. This includes work that delayed making progress after the requested due time and is not queue wait by itself. |
| `claim_wait` | Time from actual durable eligibility until the claim completes. Eligibility is the latest of `next_run_at`, the queue mutation timestamp, and an expired prior lease. |
| `dispatcher_wait` | Time from a successful durable claim until a worker is dispatched within the global and per-node budgets. |
| `total` | Time from durable eligibility until worker dispatch. |

Use `claim_wait` as the queue-delay signal. `due_lag` can include work before the
item becomes eligible and must not be reported as queue wait. Use transaction
`begin` and the pool gauges to distinguish database-pool starvation from durable
queue delay. The pool ceiling is a per-process deployment budget, so the sum
across all controld replicas and auxiliary database clients must remain below
the server connection limit.

The namespace lock in resource admission is the linearizable quota boundary.
Do not bypass it to reduce latency. Consider batched admission or a
single-candidate atomic reservation design only when repeated, same-contract
load tests show `lock_namespace` dominating the end-to-end Ready SLO after
queue and database-pool waits have been ruled out.

Dashboard service DTOs expose `diagnostic_code`, `diagnostic_message`, and
`admission_summary`. The dashboard should render those fields directly instead
of reclassifying raw backend messages in JavaScript.

The public CLI and dashboard use these stable labels:

| Surface Field | Values |
| --- | --- |
| `diagnostic_code` | `admission-blocked`, `node-selection-error`, or a concrete service runtime diagnostic such as `runtime-start-error`. |
| `admission_summary` | `namespace quota exceeded`, `node reservation capacity exhausted`, `node CPU capacity exhausted`, `node memory capacity exhausted`, `node CPU and memory capacity exhausted`, or `resource exhausted`. |

Run and service JSON output must keep these labels aligned. Table and detail
renderers may shorten presentation text, but they should not invent different
diagnostic categories.

## Boundary Rules

- `internal/kernel/resource` owns pure policy, fit evaluation, and shared
  diagnostic reason names.
- `internal/placement` owns candidate selection and placement-specific
  `ErrorInfo` construction.
- `internal/postgres/reservation` owns transactional namespace quota and node
  reservation admission.
- `internal/application/service` maps admission failures into service rollout
  degradation. It does not sum quota or node reservations.
- `internal/kernel/workload` owns shared workload diagnostic classification.
- `apps/cli/internal/workloaddiagnostic` owns CLI-side fallback classification
  for legacy/raw messages when structured workload diagnostics are unavailable.
- `axnoded` remains the runtime authority for cgroup enforcement; overcommit
  and quota do not change container limits.
- A nonzero `resources.limits.memory_bytes` adds the placement requirement
  the runtime-specific typed memory-hard-limit capability. Axnoded publishes it only after a startup probe
  creates a private cgroup and successfully writes and reads back a memory
  limit. Each admitted sandbox must still pass runtime-specific PID attribution
  after start or be force-deleted.
- `resources.requests.ephemeral_storage_bytes` participates in namespace quota,
  placement, the transactional controld ledger, and the independent axnoded
  node-local ledger. `resources.limits.ephemeral_storage_bytes` is the runtime
  hard quota. Writable roots resolve missing limit to the configured default
  and missing request to the resolved limit; readonly roots reject nonzero
  ephemeral-storage resources.
- The charged scope is the sandbox-lifetime runc writable upper or runsc
  file-backed root overlay, including metadata, copy-up, and whiteouts. It does
  not include persistent volumes, immutable lowers or image caches, artifacts,
  projection placeholders, tmpfs, or logs.
- Runsc host overhead is added only to node memory fit and persisted as
  `memory_overhead_bytes`; namespace memory quota charges the declared request.
- Runtime pool exhaustion returned by `axnoded` is a runtime-start failure, not
  an admission block. Normal saturation must be rejected by transactional
  runtime-slot admission before node dispatch.
